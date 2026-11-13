"""从教程文档里移除已下架的 wan3.0-video-prime-1080p（2026-10-25）。

幂等：每处改动先判断目标串在不在，不在就跳过；全部替换完再断言文件里不再出现该模型名。
用法：python scripts/_drop_prime_from_tutorial.py <content-models.ts> <content-models.test.ts>
"""
import sys
from pathlib import Path

PRIME = "wan3.0-video-prime-1080p"


def apply(path: Path, edits: list[tuple[str, str]]) -> None:
    text = path.read_text(encoding="utf-8")
    skipped = []
    for old, new in edits:
        if old in text:
            text = text.replace(old, new, 1)
        elif not new or new in text:
            pass  # 已应用过（删除型改动 new 为空串）
        else:
            skipped.append(old[:80])
    path.write_text(text, encoding="utf-8")
    if skipped:
        print(f"[WARN] {path.name}: {len(skipped)} 处未匹配（可能已应用）")
        for s in skipped:
            print(f"        {s!r}")
    print(f"[OK] {path.name} 已处理")


CONTENT_EDITS = [
    # ① 模型总览条目
    (
        "  'wan3.0-video-prime-1080p｜视频（按次）｜¥8.00 / 次，固定 1080P、30 秒，实时出片，"
        "参考图 ≤9 张、参考音频 ≤3 条（不支持参考视频与首尾帧）',\n",
        "",
    ),
    # ② 通用提示里的 prime 子句（字段标记规则）
    (
        "⚠️ 这几个字段在 wan3.0-smart / prime 上一律被标记为 reference_image",
        "⚠️ 这几个字段在 wan3.0-smart 上一律被标记为 reference_image",
    ),
    # ③ 通用提示里的 prime 子句（参考视频/音频的唯一通路）
    (
        "⚠️ wan3.0-smart / 官网 / prime 传参考视频、参考音频只有这一条路",
        "⚠️ wan3.0-smart / 官网 传参考视频、参考音频只有这一条路",
    ),
    # ④ metadata.audio 开关：prime 没有这个开关的说明
    (
        "；wan3.0-video-prime-1080p 走三元巡狩上游，没有这个开关",
        "",
    ),
    # ⑤ 首尾帧：prime 上游没有首尾帧入参的说明
    (
        "。⚠️ wan3.0-video-prime-1080p 走三元巡狩上游，开放接口没有首尾帧入参，传了直接 400",
        "",
    ),
    # ⑥ 计费说明
    (
        "'视频（按次）：wan3.0-video-prime-1080p 固定 ¥8 / 次，seedance-2.0",
        "'视频（按次）：seedance-2.0",
    ),
    # ⑦ 耗时实测
    (
        "wan3.0-video-prime-1080p 预计 5–15 分钟（2026-10-21 上游换为三元巡狩 WAN 3.0 实时档，"
        "暂无实测样本），",
        "",
    ),
    # ⑧ 详情章节（heading + 参数表 + hint + curl 示例）
    (
        """        {
          type: 'heading',
          level: 3,
          id: 'model-wan30-prime',
          children: [{ type: 'strong', value: 'wan3.0-video-prime-1080p（满血 30 秒档）' }],
        },
        { type: 'list', ordered: false, items: [
          ['resolution：固定 1080P，传其它值会被忽略；竖/横看 metadata.ratio（16:9 / 9:16）或 size（1920x1080 / 1080x1920），无提示默认竖'],
          ['seconds：固定 30，传其它值会被忽略'],
          ['参考素材：图 ≤9 张、音频 ≤3 条；⚠️ 不支持参考视频与首尾帧（传了直接 400，不消耗费用）'],
          ['计费：按次 ¥8.00，与时长、分辨率无关'],
        ] },
        {
          type: 'hint',
          children: [
            '2026-10-21 起上游从有赞切到三元巡狩的 WAN 3.0 实时档（上游成本 ¥4/次，售价 ¥8.00 不变，实时出片）。分辨率固定 1080P、时长固定 30 秒，不随传入参数变化。新上游的开放接口没有参考视频与首尾帧入参，素材请用参考图 + 参考音频表达。',
          ],
        },
        {
          type: 'codeBlock',
          lang: 'bash',
          value: `curl ${videoUrl} \\\\
  -H "Authorization: Bearer $NEW_API_KEY" \\\\
  -H "Content-Type: application/json" \\\\
  -d '{"model": "wan3.0-video-prime-1080p", "prompt": "海边的日落延时"}'`,
        },
""",
        "",
    ),
    # ⑨ 文档目录锚点
    ("    { title: 'wan3.0-video-prime-1080p', href: '#model-wan30-prime' },\n", ""),
]

TEST_EDITS = [
    # 在售清单里去掉
    ("      'wan3.0-video-prime-1080p',\n", ""),
    # 价格断言里去掉
    ("      '¥8.00 / 次',\n", ""),
    # 下架守卫：从「必须在售」改为「必须不在文档里」
    (
        "    assert.equal(sectionsJson.includes('wan3.0-video-prime-1080p'), true)\n",
        "",
    ),
    (
        "      'wan3.0-480p',\n    ]) {",
        "      'wan3.0-480p',\n      'wan3.0-video-prime-1080p',\n      'model-wan30-prime',\n    ]) {",
    ),
    # '固定 1080P' 只来自 prime 的参数表，随章节移除
    (
        "      // 该模型 2026-10-25 下架后随章节一并移除（见下方 de-listed 守卫）。\n"
        "      '固定 1080P',\n",
        "      // '固定 1080P' 原本来自 wan3.0-video-prime-1080p 的参数表。\n"
        "      // 这两个模型 2026-10-25 下架后随章节一并移除（见下方 de-listed 守卫）。\n",
    ),
]


def main() -> None:
    if len(sys.argv) != 3:
        raise SystemExit(__doc__)
    content = Path(sys.argv[1])
    test = Path(sys.argv[2])
    apply(content, CONTENT_EDITS)
    apply(test, TEST_EDITS)
    for p in (content, test):
        left = p.read_text(encoding="utf-8").count(PRIME)
        print(f"     {p.name}: 残留 {PRIME} {left} 处")


if __name__ == "__main__":
    main()
