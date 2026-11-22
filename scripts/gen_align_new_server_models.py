"""生成「新机对齐老机模型目录」的 SQL。

数据来源是老机生产库（逐行读出，SQL 里不手写中文/浮点，避免抄错）。
产出：scripts/prod-align-new-server-models-20261122.sql（**只在新机跑**）。

对齐口径：**老机为准**，新机补齐差异。跨机标识一律按**名字**映射，
不用 id —— 迁移后两机的渠道 id / vendor id 编号并不相同
（tj 渠道：老机 32、新机 33；阿里巴巴/MiniMax 的 vendor id 两机也相反）。
"""
import json, subprocess, sys, time, datetime

OLD = ["ssh", "-o", "ConnectTimeout=25", "-p", "877", "ubuntu@119.29.253.97"]
NEW = ["ssh", "-o", "ConnectTimeout=25", "newapi"]


def ssh(host, cmd):
    p = subprocess.run(host + [cmd], capture_output=True, timeout=180)
    return (p.stdout + b"\n" + p.stderr).decode("utf-8", "replace")


def sql(host, q):
    q = q.replace('"', '\\"')
    return ssh(host, 'sudo docker exec -i postgres psql -U root -d new-api -At -F "\x1f" -c "' + q + '"')


def rows(host, q):
    out = []
    for line in sql(host, q).split("\n"):
        if not line.strip("\x1f \t\r"):
            continue
        out.append(line.split("\x1f"))
    return out


def esc(s):
    if s is None:
        return "NULL"
    return "'" + s.replace("'", "''") + "'"


VENDOR_ICONS = {
    "OpenAI": "OpenAI", "Anthropic": "Claude.Color", "Google": "Gemini.Color",
    "Moonshot": "Moonshot", "智谱": "Zhipu.Color", "阿里巴巴": "Qwen.Color",
    "DeepSeek": "DeepSeek.Color", "MiniMax": "Minimax.Color", "百度": "Wenxin.Color",
    "讯飞": "Spark.Color", "腾讯": "Hunyuan.Color", "Cohere": "Cohere.Color",
    "Cloudflare": "Cloudflare.Color", "360": "Ai360.Color", "零一万物": "Yi.Color",
    "Jina": "Jina", "Mistral": "Mistral.Color", "xAI": "XAI", "Meta": "Ollama",
    "字节跳动": "Doubao.Color", "快手": "Kling.Color", "即梦": "Jimeng.Color", "Vidu": "Vidu",
}


def main():
    ts = int(time.time())

    # ── 渠道：按**名字**建立两机映射 ──
    old_channels = {r[0]: r for r in rows(OLD, "select name, id, models from channels")}
    new_channels = {r[0]: r for r in rows(NEW, "select name, id, models from channels")}

    # ── 老机 abilities：渠道用名字映射到新机 id ──
    old_ab = rows(OLD, "select a.model, a.\"group\", c.name, a.enabled, a.priority, a.weight, "
                      "coalesce(a.tag,'') from abilities a join channels c on c.id=a.channel_id "
                      "where a.enabled = true order by a.model")
    ab_out = []
    skipped = []
    for model, grp, cname, en, pri, w, tag in old_ab:
        if cname not in new_channels:
            skipped.append((model, cname))
            continue
        new_cid = new_channels[cname][1]
        ab_out.append({"model": model, "group": grp, "ch": new_cid,
                        "en": "true" if en in ("t", "true", "1") else "false",
                        "pri": pri, "w": w, "tag": tag, "cname": cname})

    # ── 老机 ModelPrice（全量合并：缺失的补齐、同键以老机为准）──
    old_prices = json.loads(sql(OLD, "select value from options where key='ModelPrice'").strip())
    new_prices = json.loads(sql(NEW, "select value from options where key='ModelPrice'").strip())
    price_diff = {k: (v, new_prices.get(k)) for k, v in old_prices.items() if new_prices.get(k) != v}

    # ── 老机 models 元数据（供应商按名字）──
    old_models = []
    for f in rows(OLD, "select m.model_name, m.description, m.icon, m.tags, coalesce(v.name,''), "
                       "m.endpoints, m.status, m.sync_official, m.name_rule "
                       "from models m left join vendors v on v.id=m.vendor_id "
                       "where m.deleted_at is null order by m.model_name"):
        old_models.append({"name": f[0], "desc": f[1], "icon": f[2], "tags": f[3], "vendor": f[4],
                           "endpoints": f[5], "status": f[6], "sync": f[7], "rule": f[8]})

    out = []
    out.append("-- 新机（api2）对齐老机（api）的模型目录　生成时间 %s" %
               datetime.datetime.now().strftime("%Y-%m-%d %H:%M"))
    out.append("-- 由 scripts/gen_align_new_server_models.py 自动生成，数据逐行取自老机生产库。")
    out.append("-- ⚠️ 只在新机（114.132.252.150 / api2）跑；老机不用跑。")
    out.append("-- ⚠️ 跑完等 60 秒（SyncChannelCache / SyncOptions 周期）再验证，不要立刻重启。")
    out.append("-- ⚠️ 幂等：可重复执行。")
    out.append("")
    out.append("BEGIN;")
    out.append("")
    out.append("-- ── 0. 备份 ──")
    out.append("CREATE TABLE IF NOT EXISTS bak_20261122_align_channels AS SELECT * FROM channels;")
    out.append("CREATE TABLE IF NOT EXISTS bak_20261122_align_abilities AS SELECT * FROM abilities;")
    out.append("CREATE TABLE IF NOT EXISTS bak_20261122_align_models AS SELECT * FROM models;")
    out.append("CREATE TABLE IF NOT EXISTS bak_20261122_align_options AS SELECT * FROM options;")
    out.append("")
    out.append("-- ── 1. 渠道 models 列表（按渠道**名字**对齐，逐字一致）──")
    for name, oid, models in old_channels.values():
        if name not in new_channels:
            continue
        new_cid = new_channels[name][1]
        if new_channels[name][2] == models:
            continue
        out.append("UPDATE channels SET models = %s WHERE id = %s;  -- %s" % (esc(models), new_cid, name))
    out.append("")
    out.append("-- ── 2. abilities（模型广场与路由都靠它；渠道按名字映射）──")
    for a in ab_out:
        out.append(
            "INSERT INTO abilities (model, \"group\", channel_id, enabled, priority, weight, tag) "
            "SELECT %s, %s, %s, %s, %s, %s, %s WHERE NOT EXISTS ("
            "SELECT 1 FROM abilities x WHERE x.model = %s AND x.channel_id = %s AND x.\"group\" = %s);" %
            (esc(a["model"]), esc(a["group"]), a["ch"], a["en"], a["pri"], a["w"],
             esc(a["tag"]) if a["tag"] else "NULL", esc(a["model"]), a["ch"], esc(a["group"])))
    out.append("")
    out.append("-- ── 3. ModelPrice（老机为基准；缺失的补齐，同键以老机为准）──")
    out.append("--    jsonb_build_object 的键要用单引号；它最多 100 个参数，所以分批。")
    pairs = sorted(old_prices.items())
    chunks = [pairs[i:i + 40] for i in range(0, len(pairs), 40)]
    for ci, chunk in enumerate(chunks):
        obj = ", ".join("%s, %s" % (esc(k), json.dumps(v)) for k, v in chunk)
        out.append("-- 分批合并（第 %d/%d 批）" % (ci + 1, len(chunks)))
        out.append("UPDATE options SET value = (value::jsonb || jsonb_build_object(%s))::text "
                   "WHERE key = 'ModelPrice';" % obj)
    out.append("")
    out.append("-- ── 4. models 表元数据（广场卡片：描述/标签/供应商/端点/上下架状态）──")
    out.append("--    供应商按**名字**对齐：两机 vendor id 编号不同（阿里巴巴/MiniMax 顺序相反）。")
    out.append("DO $do$")
    out.append("DECLARE v_id int;")
    out.append("BEGIN")
    for m in old_models:
        if m["vendor"]:
            out.append("  SELECT id INTO v_id FROM vendors WHERE name = %s;" % esc(m["vendor"]))
            out.append("  IF v_id IS NULL THEN")
            out.append("    INSERT INTO vendors (name, icon, status, created_time, updated_time) "
                       "VALUES (%s, %s, 1, %d, %d);" %
                       (esc(m["vendor"]), esc(VENDOR_ICONS.get(m["vendor"], "")), ts, ts))
            out.append("    SELECT id INTO v_id FROM vendors WHERE name = %s;" % esc(m["vendor"]))
            out.append("  END IF;")
        else:
            out.append("  v_id := NULL;")
        out.append("  UPDATE models SET description=%s, icon=%s, tags=%s, vendor_id=v_id, endpoints=%s, "
                   "status=%s, sync_official=%s, name_rule=%s, updated_time=%d "
                   "WHERE model_name=%s AND deleted_at IS NULL;" %
                   (esc(m["desc"]) if m["desc"] else "NULL",
                    esc(m["icon"]) if m["icon"] else "NULL",
                    esc(m["tags"]) if m["tags"] else "NULL",
                    esc(m["endpoints"]) if m["endpoints"] else "NULL",
                    m["status"], m["sync"], m["rule"], ts, esc(m["name"])))
        out.append("  INSERT INTO models (model_name, description, icon, tags, vendor_id, endpoints, "
                   "status, sync_official, name_rule, created_time, updated_time) "
                   "SELECT %s, %s, %s, %s, v_id, %s, %s, %s, %s, %d, %d WHERE NOT EXISTS ("
                   "SELECT 1 FROM models x WHERE x.model_name=%s AND x.deleted_at IS NULL);" %
                   (esc(m["name"]),
                    esc(m["desc"]) if m["desc"] else "NULL",
                    esc(m["icon"]) if m["icon"] else "NULL",
                    esc(m["tags"]) if m["tags"] else "NULL",
                    esc(m["endpoints"]) if m["endpoints"] else "NULL",
                    m["status"], m["sync"], m["rule"], ts, ts, esc(m["name"])))
    out.append("END $do$;")
    out.append("")
    out.append("COMMIT;")
    out.append("")
    out.append("-- ── 验证 ──")
    out.append("-- SELECT model||' | '||coalesce(v.name,'-')||' | st='||m.status||' | tags='||coalesce(m.tags,'-')")
    out.append("--   FROM models m LEFT JOIN vendors v ON v.id=m.vendor_id WHERE m.deleted_at IS NULL ORDER BY 1;")
    out.append("-- SELECT model, \"group\", channel_id, enabled FROM abilities ORDER BY model;")

    path = r"C:\work\new-api-src\scripts\prod-align-new-server-models-20261122.sql"
    open(path, "w", encoding="utf-8").write("\n".join(out) + "\n")
    print("written:", path)
    print("channels mapped:", len(new_channels), "abilities:", len(ab_out),
          "models rows:", len(old_models), "price keys:", len(old_prices),
          "price diffs vs new:", len(price_diff))
    if skipped:
        print("SKIPPED (新机没有同名渠道):", skipped)


if __name__ == "__main__":
    main()
