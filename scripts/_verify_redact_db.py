#!/usr/bin/env python3
"""用生产库真实数据验证客户侧脱敏（2026-10-30）。

背景：/api/task/self 与 /api/log/self 需要 session 登录态，令牌鉴权拿不到。
所以改成「取生产库的真实记录 → 本地跑一遍脱敏函数 → 看结果」。

这条路径验证的是**同一份生产数据**在出口层的处理结果，
与真实请求走的是同一套代码（model.formatUserLogs / relay.TaskModel2Dto）。

用法：
    python scripts/_verify_redact_db.py > /tmp/redact_db.json
    # 然后用本地 Go 测试读 /tmp/redact_db.json 跑脱敏
"""
import json
import subprocess

SSH = ["ssh", "-p", "877", "ubuntu@119.29.253.97"]
PG = "docker exec postgres psql -U root -d new-api -t -A"


def sql(query):
    out = subprocess.run(SSH + [f'{PG} -c "{query}"'], capture_output=True, text=True)
    if out.returncode != 0:
        raise RuntimeError(f"SQL failed: {out.stderr.strip()[:300]}\n{query}")
    return out.stdout.strip()


def rows(query):
    """跑一条返回 JSON 数组的 SQL。"""
    raw = sql(query)
    if not raw.strip():
        return []
    return json.loads(raw)


def main():
    # 最近 30 条任务：取客户侧真正能看到的字段
    tasks = rows(
        "SELECT COALESCE(json_agg(json_build_object("
        "'task_id', task_id, 'status', status, 'platform', platform,"
        "'channel_id', channel_id, 'fail_reason', fail_reason,"
        "'properties', properties, 'private_data', private_data,"
        "'data', data, 'created_at', created_at"
        ") ORDER BY created_at DESC), '[]'::json) FROM tasks "
        "WHERE created_at > EXTRACT(EPOCH FROM NOW() - INTERVAL '30 days')::bigint "
        "LIMIT 30"
    )

    # 最近 30 条日志：只取**客户可见**的类型（消费/错误/退款）。
    # type=3（管理审计）、type=4（系统）、type=7（登录）都是管理员/系统自己的记录，
    # 客户在 /api/log/self 里看不到（按 user_id 过滤，admin 的日志不在客户名下）。
    logs = rows(
        "SELECT COALESCE(json_agg(json_build_object("
        "'type', type, 'model_name', model_name, 'content', content,"
        "'channel_id', channel_id, 'channel_name', channel_name,"
        "'upstream_request_id', upstream_request_id, 'other', other,"
        "'created_at', created_at"
        ") ORDER BY created_at DESC), '[]'::json) FROM logs "
        "WHERE type IN (2, 5, 6) "
        "AND created_at > EXTRACT(EPOCH FROM NOW() - INTERVAL '30 days')::bigint "
        "LIMIT 60"
    )

    payload = {"tasks": tasks, "logs": logs}
    with open("/tmp/redact_db.json", "w", encoding="utf-8") as handle:
        json.dump(payload, handle, ensure_ascii=False)

    print(f"[dump] tasks={len(tasks)} logs={len(logs)} → /tmp/redact_db.json")

    # 快速粗筛：库里**原文**里有哪些渠道痕迹（这些正是要脱敏的对象）
    raw_text = json.dumps(payload, ensure_ascii=False).lower()
    hits = []
    for token in ("youzan", "有赞", "aliyun", "dashscope", "百炼", "wan3",
                  "tencent", "腾讯", "aliyuncs.com", "volcengine", "火山",
                  "doubao", "豆包", "manwu", "漫屋", "arcreel", "hailuo-video",
                  "r2.cloudflarestorage.com", "cos-proxy://"):
        if token.lower() in raw_text:
            hits.append((token, raw_text.count(token.lower())))
    if hits:
        print("[raw] 库内原文含渠道痕迹（这些是脱敏的靶子）：")
        for token, count in hits:
            print(f"       {token} × {count}")
    else:
        print("[raw] 库内原文未含渠道痕迹")


if __name__ == "__main__":
    main()
