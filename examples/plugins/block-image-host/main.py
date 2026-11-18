#!/usr/bin/env python3
"""Official Block plugin: block-image-host (Cloudflare R2 / S3 / GitHub image hosting)."""

import base64
import datetime
import hashlib
import hmac
import json
import mimetypes
import os
import re
import shutil
import sys
import urllib.error
import urllib.parse
import urllib.request

DEFAULT_CONFIG = {
    "provider": "r2",
    "endpoint": "",
    "bucket_prefix": "",
    "public_domain": "",
    "access_secret": "",
    "auto_upload": True,
    "post_process": "replace_keep",
}

PROVIDER_LABELS = {
    "r2": "Cloudflare R2",
    "s3": "AWS S3 兼容",
    "github": "GitHub 仓库图床",
}

VALID_PROVIDERS = {"r2", "s3", "github"}
VALID_POST_PROCESS = {"replace_keep", "replace_clean"}
MAX_LOGS = 20

IMAGE_PATTERN = re.compile(r"!\[([^\]]*)\]\(([^)\n]+)\)")
FENCE_PATTERN = re.compile(r"^\s*(```|~~~)")


class HostBridge:
    def __init__(self):
        self._seq = 1000

    def send(self, obj):
        sys.stdout.write(json.dumps(obj, ensure_ascii=False) + "\n")
        sys.stdout.flush()

    def call_host(self, method, params=None):
        self._seq += 1
        req_id = f"p-{self._seq}"
        self.send({"jsonrpc": "2.0", "id": req_id, "method": method, "params": params or {}})
        while True:
            line = sys.stdin.readline()
            if not line:
                raise RuntimeError("stdin closed while awaiting host response")
            msg = json.loads(line)
            if str(msg.get("id")) == req_id:
                if "error" in msg and msg["error"]:
                    raise RuntimeError(msg["error"].get("message", "host error"))
                return msg.get("result", {})

    def serve(self, handler):
        while True:
            line = sys.stdin.readline()
            if not line:
                break
            line = line.strip()
            if not line:
                continue
            req = json.loads(line)
            method = req.get("method")
            req_id = req.get("id")
            try:
                result = handler(self, method, req.get("params") or {})
                self.send({"jsonrpc": "2.0", "id": req_id, "result": result})
                if method == "plugin.shutdown":
                    break
            except Exception as exc:
                self.send({"jsonrpc": "2.0", "id": req_id, "error": {"code": -32000, "message": str(exc)}})


class ImageHostPlugin:
    def __init__(self):
        self.notebook_root = ""
        self.data_dir = ""
        self.locale = "zh"

    # ---- Storage helpers ----

    def _load_state(self, bridge):
        try:
            res = bridge.call_host("host.storage.get", {})
            data = res.get("data") or {}
        except Exception:
            data = {}
        raw_cfg = data.get("config") if isinstance(data.get("config"), dict) else {}
        config = self._normalize_config(raw_cfg)
        stats = data.get("stats") if isinstance(data.get("stats"), dict) else {}
        uploaded_total = int(stats.get("uploaded_total", 0) or 0)
        logs = data.get("logs") if isinstance(data.get("logs"), list) else []
        clean_logs = []
        for entry in logs[:MAX_LOGS]:
            if isinstance(entry, dict) and entry.get("title") and entry.get("time"):
                item = {"title": str(entry["title"]), "time": str(entry["time"])}
                if entry.get("status"):
                    item["status"] = str(entry["status"])
                clean_logs.append(item)
        return config, {"uploaded_total": uploaded_total}, clean_logs

    def _save_config(self, bridge, config):
        bridge.call_host("host.storage.set", {"key": "config", "value": config})

    def _save_stats(self, bridge, stats):
        bridge.call_host("host.storage.set", {"key": "stats", "value": stats})

    def _save_logs(self, bridge, logs):
        bridge.call_host("host.storage.set", {"key": "logs", "value": logs[:MAX_LOGS]})

    def _append_log(self, logs, title, status_text):
        now_str = datetime.datetime.now().strftime("%H:%M:%S")
        entry = {"title": title, "time": now_str}
        if status_text:
            entry["status"] = status_text
        return [entry] + logs[: MAX_LOGS - 1]

    def _normalize_config(self, raw):
        cfg = dict(DEFAULT_CONFIG)
        if not isinstance(raw, dict):
            return cfg
        provider = str(raw.get("provider", cfg["provider"])).strip()
        if provider in VALID_PROVIDERS:
            cfg["provider"] = provider
        for key in ("endpoint", "bucket_prefix", "public_domain", "access_secret"):
            if key in raw and raw[key] is not None:
                cfg[key] = str(raw[key]).strip()
        if "auto_upload" in raw:
            cfg["auto_upload"] = bool(raw["auto_upload"])
        post_process = str(raw.get("post_process", cfg["post_process"])).strip()
        if post_process in VALID_POST_PROCESS:
            cfg["post_process"] = post_process
        return cfg

    # ---- Markdown & local asset scanning ----

    def _parse_image_inner(self, raw_inner):
        text = raw_inner.strip()
        if not text:
            return None, ""
        if text.startswith("<"):
            end_idx = text.find(">")
            if end_idx > 1:
                target = text[1:end_idx].strip()
                suffix = text[end_idx + 1 :]
                return target, suffix
        parts = text.split(None, 1)
        target = parts[0]
        suffix = (" " + parts[1]) if len(parts) > 1 else ""
        return target, suffix

    def _normalize_local_asset_ref(self, target):
        if not target:
            return None
        lower = target.lower()
        if lower.startswith(("http://", "https://", "data:", "file:", "//")):
            return None
        clean = target.split("#", 1)[0].split("?", 1)[0]
        if clean.startswith("./"):
            clean = clean[2:]
        if clean.startswith("/") or ".." in clean.split("/"):
            return None
        if not clean.startswith("assets/"):
            return None
        return clean

    def _extract_local_refs(self, content):
        """Return list of (start, end, alt, rel_asset_path, suffix) outside fenced code blocks."""
        refs = []
        in_fence = False
        offset = 0
        for line in content.splitlines(keepends=True):
            if FENCE_PATTERN.match(line):
                in_fence = not in_fence
                offset += len(line)
                continue
            if not in_fence:
                for match in IMAGE_PATTERN.finditer(line):
                    alt = match.group(1)
                    target, suffix = self._parse_image_inner(match.group(2))
                    rel_path = self._normalize_local_asset_ref(target)
                    if rel_path:
                        refs.append(
                            (
                                offset + match.start(),
                                offset + match.end(),
                                alt,
                                rel_path,
                                suffix,
                            )
                        )
            offset += len(line)
        return refs

    def _resolve_local_file(self, note_path, rel_asset_path):
        if not self.notebook_root:
            return None
        root_real = os.path.realpath(self.notebook_root)
        unquoted = urllib.parse.unquote(rel_asset_path)
        note_dir = os.path.dirname(note_path) if note_path else ""
        candidates = []
        if note_dir:
            candidates.append(os.path.join(root_real, note_dir, unquoted))
            candidates.append(os.path.join(root_real, note_dir, rel_asset_path))
        candidates.append(os.path.join(root_real, unquoted))
        candidates.append(os.path.join(root_real, rel_asset_path))
        for cand in candidates:
            real_cand = os.path.realpath(cand)
            if real_cand == root_real or real_cand.startswith(root_real + os.sep):
                if os.path.isfile(real_cand):
                    return real_cand
        return None

    def _scan_notes(self, bridge, target_note_path=None):
        """Scan notes and return (note_count, total_refs, items_by_note)."""
        try:
            list_res = bridge.call_host("host.notes.list", {})
            notes = list_res.get("notes") or []
        except Exception:
            return 0, 0, []

        results = []
        total_refs = 0
        scanned_notes = 0
        for item in notes:
            note_path = item.get("path") if isinstance(item, dict) else None
            if not note_path:
                continue
            if target_note_path and note_path != target_note_path:
                continue
            scanned_notes += 1
            try:
                read_res = bridge.call_host("host.notes.read", {"path": note_path})
                content = read_res.get("content", "")
            except Exception:
                continue
            refs = self._extract_local_refs(content)
            if refs:
                total_refs += len(refs)
                results.append((note_path, content, refs))
        return scanned_notes, total_refs, results

    # ---- Config parsing & validation ----

    def _parse_s3_credentials(self, config):
        raw_secret = config.get("access_secret", "").strip()
        if not raw_secret:
            return None, "未配置访问密钥凭证（格式: AccessKeyId:SecretAccessKey）"
        access_key = ""
        secret_key = ""
        region = "auto" if config["provider"] == "r2" else "us-east-1"

        if raw_secret.startswith("{"):
            try:
                obj = json.loads(raw_secret)
                access_key = str(obj.get("access_key", "")).strip()
                secret_key = str(obj.get("secret_key", "")).strip()
                if obj.get("region"):
                    region = str(obj["region"]).strip()
            except Exception:
                return None, "访问密钥 JSON 格式无效"
        else:
            parts = raw_secret.split(":")
            if len(parts) < 2:
                return None, "访问密钥格式应为 AccessKeyId:SecretAccessKey（可选 :region）"
            access_key = parts[0].strip()
            secret_key = parts[1].strip()
            if len(parts) >= 3 and parts[2].strip():
                region = parts[2].strip()

        if not access_key or not secret_key:
            return None, "AccessKeyId 与 SecretAccessKey 均不能为空"

        endpoint = config.get("endpoint", "").strip()
        if not endpoint:
            return None, "请填写 S3/R2 接口地址（endpoint）"
        if not endpoint.startswith(("http://", "https://")):
            endpoint = "https://" + endpoint
        parsed = urllib.parse.urlparse(endpoint)
        if not parsed.netloc:
            return None, "接口地址格式无效"

        if config["provider"] == "s3" and region == "us-east-1":
            host_match = re.search(r"s3[.-]([a-z0-9-]+)\.amazonaws\.com", parsed.netloc)
            if host_match:
                region = host_match.group(1)

        bp = config.get("bucket_prefix", "").strip().strip("/")
        if not bp:
            return None, "请填写存储桶名称（bucket 或 bucket/prefix）"
        bp_parts = bp.split("/", 1)
        bucket = bp_parts[0].strip()
        prefix = bp_parts[1].strip("/") if len(bp_parts) > 1 else ""
        if not bucket:
            return None, "存储桶名称不能为空"

        public_domain = config.get("public_domain", "").strip().rstrip("/")
        if public_domain and not public_domain.startswith(("http://", "https://")):
            public_domain = "https://" + public_domain

        return {
            "endpoint": endpoint.rstrip("/"),
            "parsed_endpoint": parsed,
            "bucket": bucket,
            "prefix": prefix,
            "public_domain": public_domain,
            "access_key": access_key,
            "secret_key": secret_key,
            "region": region,
        }, None

    def _parse_github_credentials(self, config):
        token = config.get("access_secret", "").strip()
        if not token:
            return None, "未配置 GitHub 访问令牌（Personal Access Token）"
        repo_spec = config.get("endpoint", "").strip()
        if not repo_spec:
            return None, "请填写 GitHub 仓库标识（格式: owner/repo 或 owner/repo@branch）"
        if repo_spec.startswith(("http://", "https://")):
            parsed = urllib.parse.urlparse(repo_spec)
            repo_spec = parsed.path.strip("/")
            if repo_spec.endswith(".git"):
                repo_spec = repo_spec[:-4]
        branch = "main"
        if "@" in repo_spec:
            repo_spec, branch_part = repo_spec.split("@", 1)
            if branch_part.strip():
                branch = branch_part.strip()
        repo_parts = [p for p in repo_spec.split("/") if p]
        if len(repo_parts) != 2:
            return None, "GitHub 仓库标识格式应为 owner/repo"
        owner, repo = repo_parts[0], repo_parts[1]
        prefix = config.get("bucket_prefix", "").strip().strip("/")
        public_domain = config.get("public_domain", "").strip().rstrip("/")
        if public_domain and not public_domain.startswith(("http://", "https://")):
            public_domain = "https://" + public_domain
        return {
            "owner": owner,
            "repo": repo,
            "branch": branch,
            "prefix": prefix,
            "public_domain": public_domain,
            "token": token,
        }, None

    # ---- Network upload & connection test ----

    def _s3_signed_request(self, creds, method, path_segments, query_params=None, body=b"", content_type=None):
        parsed = creds["parsed_endpoint"]
        base_path = parsed.path.rstrip("/")
        quoted_segs = [urllib.parse.quote(seg, safe="") for seg in path_segments if seg != ""]
        canonical_uri = f"{base_path}/" + "/".join(quoted_segs)
        if not canonical_uri.startswith("/"):
            canonical_uri = "/" + canonical_uri

        query_items = sorted((k, v) for k, v in (query_params or {}).items())
        canonical_qs = "&".join(
            f"{urllib.parse.quote(k, safe='~')}={urllib.parse.quote(str(v), safe='~')}"
            for k, v in query_items
        )

        now = datetime.datetime.now(datetime.timezone.utc)
        amz_date = now.strftime("%Y%m%dT%H%M%SZ")
        date_stamp = now.strftime("%Y%m%d")
        payload_hash = hashlib.sha256(body).hexdigest()
        host_header = parsed.netloc

        headers_to_sign = {
            "host": host_header,
            "x-amz-content-sha256": payload_hash,
            "x-amz-date": amz_date,
        }
        sorted_header_keys = sorted(headers_to_sign.keys())
        canonical_headers = "".join(f"{k}:{headers_to_sign[k]}\n" for k in sorted_header_keys)
        signed_headers = ";".join(sorted_header_keys)

        canonical_request = (
            f"{method}\n"
            f"{canonical_uri}\n"
            f"{canonical_qs}\n"
            f"{canonical_headers}\n"
            f"{signed_headers}\n"
            f"{payload_hash}"
        )
        credential_scope = f"{date_stamp}/{creds['region']}/s3/aws4_request"
        string_to_sign = (
            "AWS4-HMAC-SHA256\n"
            f"{amz_date}\n"
            f"{credential_scope}\n"
            f"{hashlib.sha256(canonical_request.encode('utf-8')).hexdigest()}"
        )

        k_date = hmac.new(
            ("AWS4" + creds["secret_key"]).encode("utf-8"),
            date_stamp.encode("utf-8"),
            hashlib.sha256,
        ).digest()
        k_region = hmac.new(k_date, creds["region"].encode("utf-8"), hashlib.sha256).digest()
        k_service = hmac.new(k_region, b"s3", hashlib.sha256).digest()
        k_signing = hmac.new(k_service, b"aws4_request", hashlib.sha256).digest()
        signature = hmac.new(k_signing, string_to_sign.encode("utf-8"), hashlib.sha256).hexdigest()

        auth_header = (
            f"AWS4-HMAC-SHA256 Credential={creds['access_key']}/{credential_scope}, "
            f"SignedHeaders={signed_headers}, Signature={signature}"
        )

        url = f"{parsed.scheme}://{parsed.netloc}{canonical_uri}"
        if canonical_qs:
            url += f"?{canonical_qs}"

        req_headers = {
            "Host": host_header,
            "x-amz-content-sha256": payload_hash,
            "x-amz-date": amz_date,
            "Authorization": auth_header,
        }
        if content_type:
            req_headers["Content-Type"] = content_type
        if method == "PUT":
            req_headers["Content-Length"] = str(len(body))

        req = urllib.request.Request(url, data=body if method == "PUT" else None, headers=req_headers, method=method)
        with urllib.request.urlopen(req, timeout=15) as resp:
            return resp.status, resp.read()

    def _github_api_request(self, creds, method, api_url, payload=None):
        headers = {
            "Authorization": f"Bearer {creds['token']}",
            "Accept": "application/vnd.github+json",
            "User-Agent": "block-image-host/1.0",
        }
        data = None
        if payload is not None:
            headers["Content-Type"] = "application/json"
            data = json.dumps(payload).encode("utf-8")
        req = urllib.request.Request(api_url, data=data, headers=headers, method=method)
        with urllib.request.urlopen(req, timeout=15) as resp:
            raw = resp.read()
            return resp.status, (json.loads(raw.decode("utf-8")) if raw else {})

    def _build_remote_key(self, prefix, file_bytes, rel_asset_path):
        digest = hashlib.sha256(file_bytes).hexdigest()[:8]
        filename = os.path.basename(urllib.parse.unquote(rel_asset_path)) or "image.png"
        safe_name = re.sub(r"[^A-Za-z0-9._-]", "_", filename)
        object_name = f"{digest}-{safe_name}"
        if prefix:
            return f"{prefix}/{object_name}"
        return object_name

    def _upload_file(self, config, creds, local_path, rel_asset_path):
        with open(local_path, "rb") as fh:
            file_bytes = fh.read()
        remote_key = self._build_remote_key(creds["prefix"], file_bytes, rel_asset_path)
        content_type, _ = mimetypes.guess_type(local_path)
        content_type = content_type or "application/octet-stream"

        if config["provider"] in ("r2", "s3"):
            segments = [creds["bucket"]] + remote_key.split("/")
            self._s3_signed_request(
                creds,
                "PUT",
                segments,
                body=file_bytes,
                content_type=content_type,
            )
            encoded_key = "/".join(urllib.parse.quote(seg, safe="") for seg in remote_key.split("/"))
            if creds["public_domain"]:
                return f"{creds['public_domain']}/{encoded_key}"
            return f"{creds['endpoint']}/{urllib.parse.quote(creds['bucket'], safe='')}/{encoded_key}"

        encoded_path = "/".join(urllib.parse.quote(seg, safe="") for seg in remote_key.split("/"))
        api_url = f"https://api.github.com/repos/{creds['owner']}/{creds['repo']}/contents/{encoded_path}"
        existing_sha = None
        try:
            _, info = self._github_api_request(
                creds,
                "GET",
                f"{api_url}?ref={urllib.parse.quote(creds['branch'], safe='')}",
            )
            if isinstance(info, dict) and info.get("sha"):
                existing_sha = info["sha"]
        except urllib.error.HTTPError as err:
            if err.code != 404:
                raise

        payload = {
            "message": f"chore(image-host): upload {os.path.basename(local_path)}",
            "content": base64.b64encode(file_bytes).decode("ascii"),
            "branch": creds["branch"],
        }
        if existing_sha:
            payload["sha"] = existing_sha

        self._github_api_request(creds, "PUT", api_url, payload=payload)
        if creds["public_domain"]:
            return f"{creds['public_domain']}/{encoded_path}"
        return (
            f"https://raw.githubusercontent.com/{creds['owner']}/{creds['repo']}"
            f"/{urllib.parse.quote(creds['branch'], safe='')}/{encoded_path}"
        )

    def _test_connection(self, config):
        if config["provider"] in ("r2", "s3"):
            creds, err_msg = self._parse_s3_credentials(config)
            if err_msg:
                return False, err_msg
            try:
                self._s3_signed_request(
                    creds,
                    "GET",
                    [creds["bucket"]],
                    query_params={"list-type": "2", "max-keys": "1"},
                )
                return True, f"{PROVIDER_LABELS[config['provider']]} 连接成功（存储桶 {creds['bucket']} 可访问）"
            except urllib.error.HTTPError as err:
                return False, f"图床服务端返回 HTTP {err.code}，请核对凭证、区域或存储桶权限"
            except urllib.error.URLError as err:
                return False, f"无法连接图床服务（{err.reason}）"
            except Exception as err:
                return False, f"连接校验失败：{err}"

        creds, err_msg = self._parse_github_credentials(config)
        if err_msg:
            return False, err_msg
        try:
            api_url = f"https://api.github.com/repos/{creds['owner']}/{creds['repo']}"
            _, info = self._github_api_request(creds, "GET", api_url)
            perms = info.get("permissions") if isinstance(info, dict) else {}
            if isinstance(perms, dict) and perms.get("push") is False:
                return False, f"已连接仓库 {creds['owner']}/{creds['repo']}，但当前令牌缺少写入权限"
            return True, f"GitHub 仓库 {creds['owner']}/{creds['repo']}（分支 {creds['branch']}）连接成功"
        except urllib.error.HTTPError as err:
            return False, f"GitHub API 返回 HTTP {err.code}，请核对仓库名称或令牌权限"
        except urllib.error.URLError as err:
            return False, f"无法连接 GitHub API（{err.reason}）"
        except Exception as err:
            return False, f"连接校验失败：{err}"

    def _archive_local_files(self, bridge, archived_candidates):
        if not self.notebook_root or not archived_candidates:
            return
        _, _, remaining = self._scan_notes(bridge)
        still_referenced = set()
        for _, _, refs in remaining:
            for _, _, _, rel_path, _ in refs:
                still_referenced.add(rel_path)

        root_real = os.path.realpath(self.notebook_root)
        archive_root = os.path.join(root_real, ".image-host-archive")
        for rel_path, local_path in archived_candidates.items():
            if rel_path in still_referenced or not os.path.isfile(local_path):
                continue
            try:
                rel_from_root = os.path.relpath(local_path, root_real)
                if rel_from_root.startswith(".."):
                    continue
                dest_path = os.path.join(archive_root, rel_from_root)
                os.makedirs(os.path.dirname(dest_path), exist_ok=True)
                shutil.move(local_path, dest_path)
            except Exception:
                continue

    def _execute_upload(self, bridge, config, stats, logs, target_note_path=None):
        scanned_notes, total_refs, note_items = self._scan_notes(bridge, target_note_path=target_note_path)
        if config["provider"] in ("r2", "s3"):
            creds, err_msg = self._parse_s3_credentials(config)
        else:
            creds, err_msg = self._parse_github_credentials(config)

        if err_msg:
            status = f"已扫描 {scanned_notes} 篇笔记，发现 {total_refs} 处待上传本地图片；{err_msg}"
            return stats, logs, status, 0, total_refs

        if total_refs == 0:
            status = f"已扫描 {scanned_notes} 篇笔记，未发现待上传的本地图片"
            return stats, logs, status, 0, 0

        uploaded_cache = {}
        archived_candidates = {}
        uploaded_count = 0
        failed_count = 0
        updated_notes = 0

        for note_path, content, refs in note_items:
            pieces = []
            cursor = 0
            note_changed = False
            for start, end, alt, rel_path, suffix in refs:
                pieces.append(content[cursor:start])
                local_path = self._resolve_local_file(note_path, rel_path)
                if not local_path:
                    failed_count += 1
                    logs = self._append_log(logs, rel_path, "未找到本地文件")
                    pieces.append(content[start:end])
                    cursor = end
                    continue

                cache_key = os.path.realpath(local_path)
                if cache_key in uploaded_cache:
                    remote_url = uploaded_cache[cache_key]
                else:
                    try:
                        remote_url = self._upload_file(config, creds, local_path, rel_path)
                        uploaded_cache[cache_key] = remote_url
                        uploaded_count += 1
                        stats["uploaded_total"] = int(stats.get("uploaded_total", 0)) + 1
                        logs = self._append_log(logs, f"{note_path} -> {os.path.basename(local_path)}", "已上传")
                    except urllib.error.HTTPError as err:
                        failed_count += 1
                        logs = self._append_log(logs, os.path.basename(local_path), f"上传失败 HTTP {err.code}")
                        pieces.append(content[start:end])
                        cursor = end
                        continue
                    except urllib.error.URLError as err:
                        failed_count += 1
                        logs = self._append_log(logs, os.path.basename(local_path), f"网络不可达: {err.reason}")
                        pieces.append(content[start:end])
                        cursor = end
                        continue
                    except Exception as err:
                        failed_count += 1
                        logs = self._append_log(logs, os.path.basename(local_path), f"上传异常: {err}")
                        pieces.append(content[start:end])
                        cursor = end
                        continue

                pieces.append(f"![{alt}]({remote_url}{suffix})")
                note_changed = True
                archived_candidates[rel_path] = local_path
                cursor = end

            pieces.append(content[cursor:])
            if note_changed:
                new_content = "".join(pieces)
                try:
                    bridge.call_host("host.notes.write", {"path": note_path, "content": new_content})
                    updated_notes += 1
                except Exception as err:
                    failed_count += 1
                    logs = self._append_log(logs, note_path, f"写回笔记失败: {err}")

        if config.get("post_process") == "replace_clean" and archived_candidates:
            self._archive_local_files(bridge, archived_candidates)

        self._save_stats(bridge, stats)
        self._save_logs(bridge, logs)

        _, remaining_refs, _ = self._scan_notes(bridge)
        if failed_count > 0 and uploaded_count == 0:
            status = (
                f"已扫描 {scanned_notes} 篇笔记（{total_refs} 处本地图片），"
                f"本次 {failed_count} 项未能上传，请查看上传记录"
            )
        elif failed_count > 0:
            status = (
                f"已上传 {uploaded_count} 张图片并更新 {updated_notes} 篇笔记，"
                f"另有 {failed_count} 项失败"
            )
        else:
            status = f"成功上传 {uploaded_count} 张图片，已更新 {updated_notes} 篇笔记"
        return stats, logs, status, uploaded_count, remaining_refs

    # ---- UI Page builder ----

    def _build_page(self, config, stats, logs, pending_count, status_override=None):
        provider_label = PROVIDER_LABELS.get(config["provider"], config["provider"])
        page_status = status_override or f"{provider_label} · 待上传 {pending_count} 处"

        return {
            "title": "图床上传",
            "status": page_status,
            "header_actions": [
                {
                    "id": "upload_pending",
                    "label": "扫描并上传图片",
                    "variant": "primary",
                }
            ],
            "cards": [
                {
                    "id": "channel-card",
                    "title": "图床通道配置",
                    "items": [
                        {
                            "kind": "segmented",
                            "id": "provider",
                            "label": "图床通道",
                            "value": config["provider"],
                            "options": [
                                {"value": "r2", "label": "Cloudflare R2"},
                                {"value": "s3", "label": "AWS S3 兼容"},
                                {"value": "github", "label": "GitHub 仓库图床"},
                            ],
                        },
                        {
                            "kind": "input",
                            "id": "endpoint",
                            "label": "接口地址或仓库标识",
                            "value": config["endpoint"],
                            "placeholder": "R2/S3 填 https://<account>.r2.cloudflarestorage.com；GitHub 填 owner/repo@branch",
                        },
                        {
                            "kind": "input",
                            "id": "bucket_prefix",
                            "label": "存储桶与路径前缀",
                            "value": config["bucket_prefix"],
                            "placeholder": "R2/S3 填 bucket/prefix；GitHub 填仓库内目录前缀（如 images）",
                        },
                        {
                            "kind": "input",
                            "id": "public_domain",
                            "label": "公开访问域名",
                            "value": config["public_domain"],
                            "placeholder": "例如 https://cdn.example.com（GitHub 可留空默认 raw 地址）",
                        },
                        {
                            "kind": "secret",
                            "id": "access_secret",
                            "label": "访问密钥凭证",
                            "value": config["access_secret"],
                            "placeholder": "R2/S3 填 AccessKeyId:SecretAccessKey；GitHub 填 Personal Access Token",
                        },
                        {
                            "kind": "actions",
                            "buttons": [
                                {"id": "save_config", "label": "保存配置", "variant": "primary"},
                                {"id": "test_conn", "label": "测试连接", "variant": "secondary"},
                            ],
                        },
                    ],
                },
                {
                    "id": "policy-card",
                    "title": "自动化策略",
                    "items": [
                        {
                            "kind": "switch",
                            "id": "auto_upload",
                            "label": "保存笔记时自动上传本地图片",
                            "value": bool(config["auto_upload"]),
                        },
                        {
                            "kind": "segmented",
                            "id": "post_process",
                            "label": "上传后处理",
                            "value": config["post_process"],
                            "options": [
                                {"value": "replace_keep", "label": "替换外链并保留原图"},
                                {"value": "replace_clean", "label": "替换外链并归档原图"},
                            ],
                        },
                    ],
                },
                {
                    "id": "status-card",
                    "title": "图床状态与上传记录",
                    "items": [
                        {
                            "kind": "kv",
                            "rows": [
                                {"label": "当前通道", "value": provider_label},
                                {"label": "待上传本地图片引用数", "value": f"{pending_count} 处"},
                                {"label": "累计上传图片数", "value": f"{int(stats.get('uploaded_total', 0))} 张"},
                            ],
                        },
                        {
                            "kind": "log_list",
                            "empty_text": "暂无上传记录",
                            "entries": logs,
                        },
                    ],
                },
            ],
        }

    # ---- JSON-RPC dispatch ----

    def handle(self, bridge, method, params):
        if method == "plugin.initialize":
            self.notebook_root = str(params.get("notebook_root") or "")
            self.data_dir = str(params.get("data_dir") or "")
            self.locale = str(params.get("locale") or "zh")
            return {"ready": True}

        if method == "plugin.render":
            config, stats, logs = self._load_state(bridge)
            _, pending_count, _ = self._scan_notes(bridge)
            page = self._build_page(config, stats, logs, pending_count)
            return {"page": page}

        if method == "plugin.action":
            action_id = str(params.get("action_id") or "")
            incoming_values = params.get("values") if isinstance(params.get("values"), dict) else {}
            prev_config, stats, logs = self._load_state(bridge)
            merged = dict(prev_config)
            merged.update(incoming_values)
            config = self._normalize_config(merged)
            self._save_config(bridge, config)

            if action_id == "save_config":
                _, pending_count, _ = self._scan_notes(bridge)
                provider_label = PROVIDER_LABELS.get(config["provider"], config["provider"])
                status = f"配置已保存 · 当前通道 {provider_label}，待上传本地图片 {pending_count} 处"
                page = self._build_page(config, stats, logs, pending_count, status_override=status)
                return {"values": config, "page": page, "status": status}

            if action_id == "test_conn":
                scanned_notes, pending_count, _ = self._scan_notes(bridge)
                ok, msg = self._test_connection(config)
                if ok:
                    logs = self._append_log(logs, f"连接测试 ({PROVIDER_LABELS[config['provider']]})", "成功")
                    status = f"{msg} · 已扫描 {scanned_notes} 篇笔记（待上传 {pending_count} 处）"
                else:
                    logs = self._append_log(logs, f"连接测试 ({PROVIDER_LABELS[config['provider']]})", "未就绪")
                    status = f"{msg}（已扫描 {scanned_notes} 篇笔记，待上传 {pending_count} 处）"
                self._save_logs(bridge, logs)
                page = self._build_page(config, stats, logs, pending_count, status_override=status)
                return {"values": config, "page": page, "status": status}

            if action_id == "upload_pending":
                stats, logs, status, _, remaining_refs = self._execute_upload(bridge, config, stats, logs)
                page = self._build_page(config, stats, logs, remaining_refs, status_override=status)
                return {"values": config, "page": page, "status": status}

            _, pending_count, _ = self._scan_notes(bridge)
            status = f"未知操作：{action_id}"
            page = self._build_page(config, stats, logs, pending_count, status_override=status)
            return {"values": config, "page": page, "status": status}

        if method == "plugin.event":
            event_name = params.get("event")
            if event_name == "note.saved":
                config, stats, logs = self._load_state(bridge)
                if config.get("auto_upload"):
                    note_path = params.get("path")
                    if note_path:
                        self._execute_upload(bridge, config, stats, logs, target_note_path=str(note_path))
            return {"handled": True}

        if method == "plugin.shutdown":
            return {"ok": True}

        raise ValueError(f"unsupported method: {method}")


def main():
    plugin = ImageHostPlugin()
    bridge = HostBridge()
    bridge.serve(plugin.handle)


if __name__ == "__main__":
    main()
