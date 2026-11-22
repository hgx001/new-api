"""生成「新机对齐老机模型目录」的 SQL。

数据来源是老机生产库（逐行读出，SQL 里不手写中文/浮点，避免抄错）。
产出：scripts/prod-align-new-server-models-20261122.sql（**只在新机跑**）。
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


def esc(s):
    if s is None:
        return "NULL"
    return "'" + s.replace("'", "''") + "'"


def main():
    # ── 1. 读老机的 models 表元数据（vendor 用名字映射，不用 id：两机 vendor id 不同）──
    rows = sql(OLD, "select m.model_name, m.description, m.icon, m.tags, coalesce(v.name,''), m.endpoints, "
                    "m.status, m.sync_official, m.name_rule from models m left join vendors v on v.id=m.vendor_id "
                    "where m.deleted_at is null order by m.model_name")
    old_models = []
    for line in rows.split("\n"):
        if not line.strip("\x1f \t\r"):
            continue
        f = line.split("\x1f")
        old_models.append({
            "name": f[0], "desc": f[1], "icon": f[2], "tags": f[3], "vendor": f[4],
            "endpoints": f[5], "status": f[6], "sync": f[7], "rule": f[8],
        })

    # ── 2. 读老机的 ModelPrice（新机缺的 4 个键）──
    mp_raw = sql(OLD, "select value from options where key='ModelPrice'").strip()
    old_prices = json.loads(mp_raw)

    new_mp_raw = sql(NEW, "select value from options where key='ModelPrice'").strip()
    new_prices = json.loads(new_mp_raw)
    missing_price = {k: v for k, v in old_prices.items() if k not in new_prices}

    # ── 3. 读老机这 4 个模型的 abilities（渠道用 id 映射：两机渠道 id 相同）──
    names = list(missing_price.keys())
    inlist = ",".join("'" + n.replace("'", "''") + "'" for n in names)
    ab = sql(OLD, "select model, \"group\", channel_id, enabled, priority, weight, coalesce(tag,'') "
                  "from abilities where model in (%s) order by model" % inlist)
    old_abilities = []
    for line in ab.split("\n"):
        if not line.strip("\x1f \t\r"):
            continue
        f = line.split("\x1f")
        old_abilities.append({"model": f[0], "group": f[1], "ch": f[2], "en": f[3],
                               "pri": f[4], "w": f[5], "tag": f[6]})

    # ── 4. 老机承载这些模型的渠道 models 列表（新机要补同样的字符串）──
    chans = sql(OLD, "select distinct channel_id from abilities where model in (%s)" % inlist)
    chan_ids = [c.strip() for c in chans.strip().split("\n") if c.strip()]
    chan_models = {}
    for cid in chan_ids:
        v = sql(OLD, "select models from channels where id=%s" % cid).strip()
        chan_models[cid] = v

    ts = int(time.time())
    out = []
    out.append("-- 新机（api2）对齐老机（api）的模型目录　生成时间 %s" %
               datetime.datetime.now().strftime("%Y-%m-%d %H:%M"))
    out.append("-- 由 scripts/gen_align_new_server_models.py 自动生成，数据逐行取自老机生产库。")
    out.append("-- ⚠️ 只在新机（114.132.252.150 / api2）跑；老机不用跑。")
    out.append("-- ⚠️ 跑完等 60 秒（SyncChannelCache / SyncOptions 周期）再验证，不要立刻重启。")
    out.append("")
    out.append("BEGIN;")
    out.append("")
    out.append("-- ── 0. 备份 ──")
    out.append("CREATE TABLE IF NOT EXISTS bak_20261122_align_channels AS SELECT * FROM channels;")
    out.append("CREATE TABLE IF NOT EXISTS bak_20261122_align_abilities AS SELECT * FROM abilities;")
    out.append("CREATE TABLE IF NOT EXISTS bak_20261122_align_models AS SELECT * FROM models;")
    out.append("CREATE TABLE IF NOT EXISTS bak_20261122_align_options AS SELECT * FROM options;")
    out.append("")
    out.append("-- ── 1. 渠道 models 列表对齐（漫屋 ch20 少了 4 个模型）──")
    out.append("--    直接抄老机的完整列表，保证两台字符串逐字一致（差一个逗号都会让后续 diff 噪声变大）。")
    for cid, mlist in chan_models.items():
        add = [n for n in names if n in mlist]
        if not add:
            continue
        out.append("UPDATE channels SET models = %s WHERE id = %s;" % (esc(mlist), cid))
    out.append("")
    out.append("-- ── 2. abilities（直接改 channels.models 不会自动同步能力行）──")
    for a in old_abilities:
        out.append(
            "INSERT INTO abilities (model, \"group\", channel_id, enabled, priority, weight, tag) "
            "SELECT %s, %s, %s, %s, %s, %s, %s WHERE NOT EXISTS ("
            "SELECT 1 FROM abilities x WHERE x.model = %s AND x.channel_id = %s AND x.\"group\" = %s);" %
            (esc(a["model"]), esc(a["group"]), a["ch"],
             "true" if a["en"] in ("t", "true", "1") else "false",
             a["pri"], a["w"],
             esc(a["tag"]) if a["tag"] else "NULL", esc(a["model"]), a["ch"], esc(a["group"])))
    out.append("")
    out.append("-- ── 3. ModelPrice（老机为基准；新机少的 4 个键补齐，同键按老机取值覆盖）──")
    # jsonb_build_object 的键要用**单引号**（双引号会被当成 SQL 标识符）；
    # 而且它最多接受 100 个参数（= 50 对），所以要分批。
    pairs = sorted(old_prices.items())
    chunks = [pairs[i:i + 40] for i in range(0, len(pairs), 40)]
    for ci, chunk in enumerate(chunks):
        obj = ", ".join("%s, %s" % (esc(k), json.dumps(v)) for k, v in chunk)
        out.append("-- ModelPrice 分批合并（第 %d/%d 批）" % (ci + 1, len(chunks)))
        out.append("UPDATE options SET value = (value::jsonb || jsonb_build_object(%s))::text WHERE key = 'ModelPrice';" % obj)
    out.append("")
    out.append("-- ── 4. models 表元数据（广场卡片：描述/标签/供应商/端点/上下架状态）──")
    out.append("--    供应商按**名字**对齐：两机 vendor id 编号不同（阿里巴巴/MiniMax 顺序相反）。")
    out.append("DO $do$")
    out.append("DECLARE v_id int;")
    out.append("BEGIN")
    for m in old_models:
        vendor = m["vendor"]
        if vendor:
            out.append("  SELECT id INTO v_id FROM vendors WHERE name = %s;" % esc(vendor))
            out.append("  IF v_id IS NULL THEN")
            out.append("    INSERT INTO vendors (name, icon, status, created_time, updated_time) "
                       "VALUES (%s, %s, 1, %d, %d);" % (esc(vendor), esc(vendor_icon(vendor)), ts, ts))
            out.append("    SELECT id INTO v_id FROM vendors WHERE name = %s;" % esc(vendor))
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
    out.append("-- SELECT model||' | '||coalesce(v.name,'-')||' | st='||status||' | tags='||coalesce(tags,'-') "
               "FROM models m LEFT JOIN vendors v ON v.id=m.vendor_id WHERE deleted_at IS NULL ORDER BY 1;")
    out.append("-- SELECT model, \"group\", channel_id, enabled FROM abilities WHERE model IN "
               "('el-seedance-2.5flash','el-seedance-2.0mini','el-gemini-1.2flash','seedance-2-0');")
    out.append("-- SELECT key FROM options WHERE key='ModelPrice' AND value::jsonb ? 'el-seedance-2.5flash';")

    path = r"C:\work\new-api-src\scripts\prod-align-new-server-models-20261122.sql"
    open(path, "w", encoding="utf-8").write("\n".join(out) + "\n")
    print("written:", path)
    print("models rows:", len(old_models), "missing price keys:", len(missing_price),
          "abilities:", len(old_abilities), "channels:", list(chan_models))


def vendor_icon(name):
    return {
        "OpenAI": "OpenAI", "Anthropic": "Claude.Color", "Google": "Gemini.Color",
        "Moonshot": "Moonshot", "智谱": "Zhipu.Color", "阿里巴巴": "Qwen.Color",
        "DeepSeek": "DeepSeek.Color", "MiniMax": "Minimax.Color", "百度": "Wenxin.Color",
        "讯飞": "Spark.Color", "腾讯": "Hunyuan.Color", "Cohere": "Cohere.Color",
        "Cloudflare": "Cloudflare.Color", "360": "Ai360.Color", "零一万物": "Yi.Color",
        "Jina": "Jina", "Mistral": "Mistral.Color", "xAI": "XAI", "Meta": "Ollama",
        "字节跳动": "Doubao.Color", "快手": "Kling.Color", "即梦": "Jimeng.Color", "Vidu": "Vidu",
    }.get(name, "")


if __name__ == "__main__":
    main()
