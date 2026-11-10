#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""节点 agent 的纯逻辑单测：不联网、不碰 sing-box，只锁住配对与用户合并规则。

跑法：python3 -m unittest discover -s deploy/vpn-node-agent -p 'test_*.py'
"""

import importlib.util
import json
import os
import shutil
import tempfile
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))
spec = importlib.util.spec_from_file_location("vpn_agent", os.path.join(HERE, "agent.py"))
agent = importlib.util.module_from_spec(spec)
spec.loader.exec_module(agent)

LEGACY = [
    {"uuid": "d316bab5-0000-0000-0000-000000000000", "flow": "xtls-rprx-vision"},
    {"uuid": "c0aac922-0000-0000-0000-000000000000", "flow": "xtls-rprx-vision"},
]


class BuildUsersTest(unittest.TestCase):
    def test_keeps_legacy_and_rebuilds_managed_only(self):
        existing = LEGACY + [{"name": "v3", "uuid": "old-uuid", "flow": "xtls-rprx-vision"}]
        manifest = [{"name": "v7", "uuid": "new-uuid-7"}, {"name": "v9", "uuid": "new-uuid-9"}]
        users, kept, managed = agent.build_users(existing, manifest)

        self.assertEqual(kept, LEGACY, "老共享用户必须原样保留且不带 name")
        self.assertEqual([u["name"] for u in managed], ["v7", "v9"])
        self.assertEqual([u["uuid"] for u in managed], ["new-uuid-7", "new-uuid-9"])
        self.assertEqual(len(users), 4)
        for user in managed:
            self.assertEqual(user["flow"], "xtls-rprx-vision", "新用户必须继承入站 flow")

    def test_rejects_malformed_manifest_entries(self):
        users, _, managed = agent.build_users(LEGACY, [{"name": "hacker", "uuid": "x"}, {"name": "v1", "uuid": ""}])
        self.assertEqual(managed, [])
        self.assertEqual(users, LEGACY)

    def test_empty_manifest_removes_all_managed_users(self):
        existing = LEGACY + [{"name": "v3", "uuid": "old-uuid", "flow": "xtls-rprx-vision"}]
        users, _, managed = agent.build_users(existing, [])
        self.assertEqual(managed, [])
        self.assertEqual(users, LEGACY)


class CollectEventsTest(unittest.TestCase):
    def test_pairs_source_ip_with_user_by_connection_id(self):
        lines = [
            "+0800 2026-11-10 09:44:29 INFO [527659267 0ms] inbound/vless[jp-vless-in]: inbound connection from 203.0.113.9:52854",
            "+0800 2026-11-10 09:44:29 INFO [527659267 0ms] inbound/vless[jp-vless-in]: [v7] inbound connection to www.apple.com:443",
            "+0800 2026-11-10 09:44:30 INFO [527659268 0ms] inbound/vless[jp-vless-in]: inbound connection from 198.51.100.4:44444",
            "+0800 2026-11-10 09:44:30 INFO [527659268 0ms] inbound/vless[jp-vless-in]: [d316] inbound connection to example.com:443",
        ]
        events = agent.collect_events(lines)
        self.assertEqual(events, [{"name": "v7", "ip": "203.0.113.9"}], "非托管用户名不能上报")

    def test_ignores_interleaved_connections(self):
        lines = [
            "[1 0ms] inbound/vless[jp-vless-in]: inbound connection from 10.0.0.1:1000",
            "[2 0ms] inbound/vless[jp-vless-in]: inbound connection from 10.0.0.2:2000",
            "[2 1ms] inbound/vless[jp-vless-in]: [v1] inbound connection to a.com:443",
            "[1 1ms] inbound/vless[jp-vless-in]: [v2] inbound connection to b.com:443",
        ]
        events = agent.collect_events(lines)
        self.assertEqual(
            sorted(events, key=lambda e: e["name"]),
            [{"name": "v1", "ip": "10.0.0.2"}, {"name": "v2", "ip": "10.0.0.1"}],
        )

    def test_filters_by_inbound_tag(self):
        lines = [
            "[1 0ms] inbound/vless[other]: inbound connection from 10.0.0.9:1000",
            "[1 0ms] inbound/vless[other]: [v1] inbound connection to a.com:443",
        ]
        self.assertEqual(agent.collect_events(lines, tag="jp-vless-in"), [])

    def test_handles_ipv6_source(self):
        lines = [
            "[9 0ms] inbound/vless[jp-vless-in]: inbound connection from 2001:db8::1:4433",
            "[9 0ms] inbound/vless[jp-vless-in]: [v5] inbound connection to a.com:443",
        ]
        self.assertEqual(agent.collect_events(lines)[0]["ip"], "2001:db8::1")


class LogCursorTest(unittest.TestCase):
    def setUp(self):
        self.dir = tempfile.mkdtemp()
        self.log_path = os.path.join(self.dir, "test.log")
        self.cfg = agent.Config(os.path.join(self.dir, "missing.env"))
        self.cfg.log_file = self.log_path
        self.cfg.state_dir = self.dir

    def tearDown(self):
        shutil.rmtree(self.dir, ignore_errors=True)

    def write(self, text):
        with open(self.log_path, "w", encoding="utf-8") as handle:
            handle.write(text)

    def test_first_run_starts_at_end(self):
        self.write("old line\n")
        lines, state = agent.read_new_lines(self.cfg, None)
        self.assertEqual(lines, [])
        self.assertEqual(state["offset"], os.path.getsize(self.log_path))

    def test_reads_only_new_lines(self):
        self.write("a\n")
        _, state = agent.read_new_lines(self.cfg, None)
        with open(self.log_path, "a", encoding="utf-8") as handle:
            handle.write("b\n")
        lines, state = agent.read_new_lines(self.cfg, state)
        self.assertEqual(lines, ["b"])

    def test_partial_line_is_kept_for_next_round(self):
        self.write("a\n")
        _, state = agent.read_new_lines(self.cfg, None)
        with open(self.log_path, "a", encoding="utf-8") as handle:
            handle.write("half")
        lines, state = agent.read_new_lines(self.cfg, state)
        self.assertEqual(lines, [])
        with open(self.log_path, "a", encoding="utf-8") as handle:
            handle.write(" done\n")
        lines, _ = agent.read_new_lines(self.cfg, state)
        self.assertEqual(lines, ["half done"])

    def test_truncation_resets_offset(self):
        self.write("aaaa\n")
        _, state = agent.read_new_lines(self.cfg, None)
        self.write("b\n")
        lines, _ = agent.read_new_lines(self.cfg, state)
        self.assertEqual(lines, ["b"])

    def test_rotation_reads_tail_of_rotated_file_then_new_file(self):
        self.write("a\n")
        _, state = agent.read_new_lines(self.cfg, None)
        # 模拟 logrotate：rename + create，sing-box 仍往旧 inode 写
        rotated = self.log_path + ".1"
        os.replace(self.log_path, rotated)
        with open(rotated, "a", encoding="utf-8") as handle:
            handle.write("b\n")
        self.write("c\n")

        lines, _ = agent.read_new_lines(self.cfg, state)
        self.assertIn("b", lines, "轮转后旧文件里的尾巴不能丢")
        self.assertIn("c", lines, "新文件从头读")

    def test_compressed_rotated_file_is_skipped_without_crash(self):
        self.write("a\n")
        _, state = agent.read_new_lines(self.cfg, None)
        rotated = self.log_path + ".1.gz"
        os.replace(self.log_path, rotated)
        self.write("d\n")
        lines, _ = agent.read_new_lines(self.cfg, state)
        self.assertEqual(lines, ["d"])


class PlanChangeTest(unittest.TestCase):
    def setUp(self):
        self.dir = tempfile.mkdtemp()
        self.cfg = agent.Config(os.path.join(self.dir, "missing.env"))
        self.cfg.singbox_config = os.path.join(self.dir, "config.json")
        self.cfg.inbound_tag = "jp-vless-in"

    def tearDown(self):
        shutil.rmtree(self.dir, ignore_errors=True)

    def write_config(self, users):
        config = {
            "log": {"level": "info"},
            "inbounds": [
                {"type": "vless", "tag": "jp-vless-in", "listen_port": 8443, "users": users},
                {"type": "socks", "tag": "other", "listen_port": 1080},
            ],
            "outbounds": [{"type": "direct", "tag": "direct"}],
        }
        with open(self.cfg.singbox_config, "w", encoding="utf-8") as handle:
            json.dump(config, handle, indent=2)
        return config

    def test_no_change_when_manifest_matches(self):
        self.write_config(LEGACY + [{"name": "v7", "uuid": "u7", "flow": "xtls-rprx-vision"}])
        summary, candidate = agent.plan_change(self.cfg, {"users": [{"name": "v7", "uuid": "u7"}]}, False)
        self.assertFalse(summary["changed"])
        self.assertIsNone(candidate)

    def test_rewrites_only_target_inbound_and_keeps_other_inbounds(self):
        self.write_config(LEGACY)
        summary, candidate = agent.plan_change(self.cfg, {"users": [{"name": "v7", "uuid": "u7"}]}, False)
        self.assertTrue(summary["changed"])
        parsed = json.loads(candidate)
        inbound = parsed["inbounds"][0]
        self.assertEqual([u.get("name") for u in inbound["users"]], [None, None, "v7"])
        self.assertEqual(parsed["inbounds"][1]["listen_port"], 1080, "其它入站不能被动到")
        self.assertEqual(parsed["outbounds"], [{"type": "direct", "tag": "direct"}])

    def test_fails_loudly_when_inbound_missing(self):
        self.write_config(LEGACY)
        self.cfg.inbound_tag = "nope"
        with self.assertRaises(SystemExit):
            agent.plan_change(self.cfg, {"users": []}, False)


if __name__ == "__main__":
    unittest.main()
