# 深层次界面

## 输出

- 输出 ```ui 围栏，内含单行 JSON：`{"title"?, "blocks":[...], "foot"?}`，每块用 `t` 指定组件，可选 `title`、`sub`。
- 按内容挑组件、自由组合，同一回答优先 2–4 种组件搭配；用 `group` 并排、`tabs` 分视图、`fold` 收起次要细节。
- 每块短句、≤6 项；数值写数字。组件里不堆砌大段代码，简短命令可放步骤或卡片说明，完整代码另用围栏外代码块。
- 内容全放进组件内；围栏外正文 ≤3 行（仅结论、前提说明、待确认事项或单列完整代码块），不复述画面，也不要再用 Markdown 分节展开。

## 组件

| t | 字段 | 适合 |
| --- | --- | --- |
| `kpi` | `items:[{l,v,tag?,s?,tone?}]` | 关键指标 |
| `bars` | `items:[{l,v,s?,tone?}]`、`unit?` | 数量对比、排行 |
| `progress` | `items:[{l,v}]`（0–100） | 完成度、占比进度 |
| `line` | `x:[..]`、`series:[{name,vals:[..]}]`、`yUnit?` | 趋势 |
| `share` | `items:[{l,v}]` | 构成占比（环形图） |
| `table` | `cols:[..]`、`rows:[[..]]` | 多维明细 |
| `cards` | `items:[{name,badge?,pick?,desc?,pts:[..],tone?}]` | 分组卡片、方案对比、推荐 |
| `proscons` | `pros:[..]`、`cons:[..]` | 利弊权衡 |
| `steps` | `items:[{l,s?,state:done/doing/todo}]` | 流程、进度、时间线 |
| `checklist` | `items:[{l,ok}]` | 核对清单 |
| `tags` | `items:[..]` 或 `[{l,tone}]` | 关键词、分类 |
| `callout` | `tone`、`title?`、`text` | 结论、风险、提醒 |
| `text` | `text` 或 `items:[..]` | 短段落、要点 |
| `group` | `blocks:[..]`、`cols?` | 多块并排 |
| `tabs` | `items:[{label,blocks:[..]}]` | 同类多视图切换 |
| `fold` | `title`、`blocks:[..]`、`open?` | 次要细节默认收起 |

`tone` 取 `ok`、`warn`、`bad`、`info`。

## 示例

```ui
{"title":"三款方案","blocks":[{"t":"kpi","items":[{"l":"推荐","v":"方案 B","tone":"ok"},{"l":"预算","v":"¥3,200"}]},{"t":"group","blocks":[{"t":"bars","title":"评分","items":[{"l":"方案 A","v":72},{"l":"方案 B","v":88}]},{"t":"proscons","pros":["上手快"],"cons":["费用高"]}]}]}
```
