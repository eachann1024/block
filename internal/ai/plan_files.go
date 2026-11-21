package ai

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"block/internal/files"
	"block/internal/model"
	"block/internal/theme"
)

type planFileChange struct {
	name            string
	before, after   string
	existed, exists bool
}

func planFileChanges(plan *model.BatchPlan, undo bool) ([]planFileChange, error) {
	if plan == nil || len(plan.Ops) == 0 || len(plan.Ops) > maxPlanPages {
		return nil, errors.New(theme.Tr("无效的 AI 修改计划", "Invalid AI edit plan"))
	}
	var changes []planFileChange
	seen := make(map[string]bool)
	for _, op := range plan.Ops {
		name, err := noteFilename(op.Page)
		if err != nil {
			return nil, err
		}
		expected, ok := plan.Expected[op.Page]
		if !ok || expected != op.Old || seen[name] || len(op.New) > maxToolNoteBytes ||
			(op.Kind != model.PlanOpReplace && op.Kind != model.PlanOpCreateNote) {
			return nil, errors.New(theme.Tr("计划缺少可验证的原文", "The plan is missing verifiable original text"))
		}
		seen[name] = true
		create := op.Kind == model.PlanOpCreateNote
		if create && expected != "" {
			return nil, errors.New(theme.Tr("新建笔记计划包含原文", "A create-note plan includes original text"))
		}
		if !checkMarkdownPreflight(expected, op.New) {
			return nil, errors.New(theme.Tr("所选修改包含新增的不支持 Markdown 结构，请调整后重试。", "Selected changes introduce unsupported Markdown."))
		}
		change := planFileChange{name: name, before: expected, after: op.New, existed: !create, exists: true}
		if undo {
			change = change.reverse()
		}
		changes = append(changes, change)
	}
	return changes, nil
}

func (c planFileChange) reverse() planFileChange {
	return planFileChange{name: c.name, before: c.after, after: c.before, existed: c.exists, exists: c.existed}
}

func (c planFileChange) check(dir *os.Root) error {
	text, exists, err := snapshotNotebookNote(context.Background(), dir, c.name)
	if err != nil || exists != c.existed || text != c.before {
		return fmt.Errorf("%s: %w", displayPageID(c.name), errors.New(theme.Tr("笔记原文或位置已变化，请重新审阅", "Note content or location changed. Review it again.")))
	}
	return nil
}

func (c planFileChange) write(dir *os.Root) error {
	if err := c.check(dir); err != nil {
		return err
	}
	if !c.exists {
		return dir.Remove(c.name)
	}
	if c.existed {
		return files.WriteNoteAt(dir, c.name, c.after)
	}

	// Link a complete temporary file into place without replacing any existing target.
	temp := filepath.Join(filepath.Dir(c.name), ".block-ai-"+rand.Text()+".tmp")
	if err := files.WriteNoteAt(dir, temp, c.after); err != nil {
		return err
	}
	defer dir.Remove(temp)
	return dir.Link(temp, c.name)
}

// ApplyPlanFiles validates every page before writing and conditionally rolls back a partial batch.
func ApplyPlanFiles(host model.WorkspaceHost, plan *model.BatchPlan, undo bool) error {
	changes, err := planFileChanges(plan, undo)
	if err != nil {
		return err
	}
	root, err := AuthorizedNotebookRoot(host, plan.Root)
	if err != nil || root != plan.Root {
		return errors.New(theme.Tr("计划的笔记本已失去授权", "The plan's notebook is no longer authorized"))
	}
	dir, err := openNotebook(root)
	if err != nil {
		return err
	}
	defer dir.Close()
	for _, change := range changes {
		if err := change.check(dir); err != nil {
			return err
		}
	}
	for i, change := range changes {
		if change.existed && change.exists && change.before == change.after {
			continue
		}
		if err := change.write(dir); err != nil {
			var rollbackErr error
			for j := i - 1; j >= 0; j-- {
				old := changes[j]
				if old.existed && old.exists && old.before == old.after {
					continue
				}
				rollbackErr = errors.Join(rollbackErr, old.reverse().write(dir))
			}
			if rollbackErr != nil {
				return errors.Join(err, fmt.Errorf("%s: %w", theme.Tr("部分笔记无法撤回，请检查文件", "Some notes could not be reverted. Check the files."), rollbackErr))
			}
			return err
		}
	}
	return nil
}

// LocatePlanNote returns the validated absolute note path and whether it exists.
func LocatePlanNote(host model.WorkspaceHost, root string, page string) (string, bool, error) {
	canonical, workspaceRoot, err := resolveNotebook(host, root)
	if err != nil || canonical != root {
		return "", false, errors.New(theme.Tr("计划的笔记本已失去授权", "The plan's notebook is no longer authorized"))
	}
	name, err := noteFilename(page)
	if err != nil {
		return "", false, err
	}
	dir, err := openNotebook(root)
	if err != nil {
		return "", false, err
	}
	defer dir.Close()
	_, err = inspectNote(dir, name)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", false, err
	}
	return filepath.Join(workspaceRoot, name), err == nil, nil
}

// ReadPlanPreview checks the original before the editor opens a plan's first note.
func ReadPlanPreview(ctx context.Context, host model.WorkspaceHost, plan *model.BatchPlan) (string, string, bool, error) {
	if err := ctx.Err(); err != nil {
		return "", "", false, err
	}
	changes, err := planFileChanges(plan, false)
	if err != nil {
		return "", "", false, err
	}
	root, workspaceRoot, err := resolveNotebook(host, plan.Root)
	if err != nil || root != plan.Root {
		return "", "", false, errors.New(theme.Tr("计划的笔记本已失去授权", "The plan's notebook is no longer authorized"))
	}
	dir, err := openNotebook(root)
	if err != nil {
		return "", "", false, err
	}
	defer dir.Close()
	first := changes[0]
	text, exists, err := snapshotNotebookNote(ctx, dir, first.name)
	if err != nil || exists != first.existed || text != first.before {
		return "", "", false, errors.New(theme.Tr("笔记原文或位置已变化，请重新审阅", "Note content or location changed. Review it again."))
	}
	return filepath.Join(workspaceRoot, first.name), text, exists, nil
}
