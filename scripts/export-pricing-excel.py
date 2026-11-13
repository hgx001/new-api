#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""Export the production Model Square into a formatted Excel workbook.

Pricing semantics mirror web/default/src/features/pricing/lib/price.ts so the
numbers match what the Model Square cards show:

  token-based (quota_type=0): USD / 1M tokens = model_ratio * 2 * group_ratio
                              output            = base * completion_ratio
                              cached            = base * cache_ratio
  per-request (quota_type=1): USD / call        = model_price * group_ratio

The card shows the minimum group ratio across a model's enabled groups, and
converts USD to the display currency with /api/status usd_exchange_rate.

Usage:
    python scripts/export-pricing-excel.py [--api https://api.heibaidao.cn]
                                            [--out <path.xlsx>]
"""

from __future__ import annotations

import argparse
import datetime as dt
import json
import math
import os
import urllib.request

from openpyxl import Workbook
from openpyxl.styles import Alignment, Border, Font, PatternFill, Side
from openpyxl.utils import get_column_letter

# --- palette -----------------------------------------------------------------
NAVY = "1F3864"
BLUE = "2E5C8A"
BAND = "F4F7FC"
LINE = "B4C6E7"
NOTE = "5B6B84"

FONT = "Microsoft YaHei"
TITLE_FONT = Font(name=FONT, size=16, bold=True, color=NAVY)
SUB_FONT = Font(name=FONT, size=9, color=NOTE)
HEAD_FONT = Font(name=FONT, size=10, bold=True, color="FFFFFF")
BODY_FONT = Font(name=FONT, size=10)
BODY_BOLD = Font(name=FONT, size=10, bold=True)
MONO_FONT = Font(name="Consolas", size=10)

HEAD_FILL = PatternFill("solid", fgColor=NAVY)
BAND_FILL = PatternFill("solid", fgColor=BAND)
SECTION_FILL = PatternFill("solid", fgColor=BLUE)

_thin = Side(style="thin", color=LINE)
BORDER = Border(left=_thin, right=_thin, top=_thin, bottom=_thin)

MONEY_TOKEN = "¥#,##0.0000"
MONEY_UNIT = "¥#,##0.00##"
CENTER = Alignment(horizontal="center", vertical="center")
LEFT = Alignment(horizontal="left", vertical="center")
WRAP_LEFT = Alignment(horizontal="left", vertical="center", wrap_text=True)
RIGHT = Alignment(horizontal="right", vertical="center")


# --- data helpers ------------------------------------------------------------
def fetch_json(url: str) -> dict:
    req = urllib.request.Request(
        url, headers={"User-Agent": "Mozilla/5.0 (new-api pricing export)"}
    )
    with urllib.request.urlopen(req, timeout=30) as resp:
        return json.loads(resp.read().decode("utf-8"))


def min_group_ratio(enable_groups, group_ratio) -> float:
    """Mirror of getMinGroupRatio() in web/default/src/features/pricing/lib/price.ts."""
    if not enable_groups:
        return 1.0
    ratios = [group_ratio[g] for g in enable_groups if g in group_ratio]
    return min(ratios) if ratios else 1.0


def is_per_second_model(name: str) -> bool:
    """Mirror of isPerSecondModel() in web/default/src/features/pricing/lib/model-helpers.ts."""
    if name == "wan3.0-video-prime-1080p":
        return False
    if name.startswith("seedance-2-0"):
        return True
    if name == "wan3.0-480p":
        return True
    return (
        name.startswith("wan3.0-video")
        or name.startswith("wan3.0-smart")
        or name.startswith("wan2.7-r2v")
        or name.startswith("autodl:")
    )


def fmt_price(value, digits_large: int = 4, digits_small: int = 6) -> str:
    """Mirror of formatCurrencyValue(): >=1 keeps 4 decimals, <1 keeps 6, trailing zeros stripped."""
    if value is None:
        return "-"
    digits = digits_large if abs(value) >= 1 else digits_small
    text = f"{value:.{digits}f}"
    if "." in text:
        text = text.rstrip("0").rstrip(".")
    return text or "0"


def money(value, digits_large: int = 4, digits_small: int = 4) -> str:
    return "¥" + fmt_price(value, digits_large, digits_small)


def is_text_model(model: dict) -> bool:
    """Token-billed models (quota_type = 0) are the chat / text models."""
    return model.get("quota_type", 0) == 0


def seedance_factor(name: str, multiplier: float) -> float:
    """Seedance family models carry a configurable price multiplier."""
    if multiplier == 1.0:
        return 1.0
    return multiplier if "seedance" in (name or "").lower() else 1.0


def compute_prices(
    model: dict,
    group_ratio: dict,
    rate: float,
    group: str | None = None,
    multiplier: float = 1.0,
):
    """Return the price bundle for one model under one group (None = cheapest enabled group)."""
    enable_groups = model.get("enable_groups") or []
    if group is None:
        ratio = min_group_ratio(enable_groups, group_ratio)
    else:
        ratio = group_ratio.get(group, 1.0)

    factor = seedance_factor(model.get("model_name", ""), multiplier)
    quota_type = model.get("quota_type", 0)
    if quota_type == 0:
        base = (model.get("model_ratio") or 0) * 2 * ratio
        out = {
            "kind": "token",
            "input": round(base * rate * factor, 8),
            "output": round(base * (model.get("completion_ratio") or 0) * rate * factor, 8),
            "cache": (
                round(base * model["cache_ratio"] * rate * factor, 8)
                if model.get("cache_ratio") is not None
                else None
            ),
            "fixed": None,
            "ratio": ratio,
            "factor": factor,
        }
    else:
        out = {
            "kind": "request",
            "input": None,
            "output": None,
            "cache": None,
            "fixed": round((model.get("model_price") or 0) * ratio * rate * factor, 8),
            "ratio": ratio,
            "factor": factor,
        }
    out["per_second"] = quota_type == 1 and is_per_second_model(model.get("model_name", ""))
    return out


def display_width(text: str) -> int:
    """Rough on-screen width: CJK/full-width glyphs count as 2 columns."""
    return sum(2 if ord(ch) > 0x2E7F else 1 for ch in text)


def fitted_height(text: str, column_width: int, base: int = 28, line: int = 14) -> int:
    """Row height that fits the wrapped text of a cell instead of clipping it."""
    if not text:
        return base
    lines = max(1, math.ceil(display_width(text) / max(1, column_width - 2)))
    return min(max(base, lines * line + 8), 150)


def card_summary(model: dict, prices: dict) -> str:
    """One-line quote exactly as the Model Square card badge reads."""
    if prices["kind"] == "token":
        parts = [
            f"输入 {money(prices['input'], 4, 6)}/1M",
            f"输出 {money(prices['output'], 4, 6)}/1M",
        ]
        if prices["cache"] is not None:
            parts.append(f"缓存 {money(prices['cache'], 4, 6)}/1M")
        return " · ".join(parts)
    unit = "秒" if prices["per_second"] else "次"
    return f"{money(prices['fixed'], 4, 4)}/{unit}"


# --- sheet builders ----------------------------------------------------------
def style_header(ws, row: int, headers: list[str], widths: list[int]) -> None:
    for idx, (head, width) in enumerate(zip(headers, widths), start=1):
        cell = ws.cell(row=row, column=idx, value=head)
        cell.font = HEAD_FONT
        cell.fill = HEAD_FILL
        cell.alignment = Alignment(horizontal="center", vertical="center", wrap_text=True)
        cell.border = BORDER
        ws.column_dimensions[get_column_letter(idx)].width = width
    ws.row_dimensions[row].height = 30


def write_title(ws, title: str, subtitle: str, span: int) -> None:
    ws.merge_cells(start_row=1, start_column=1, end_row=1, end_column=span)
    cell = ws.cell(row=1, column=1, value=title)
    cell.font = TITLE_FONT
    cell.alignment = LEFT
    ws.row_dimensions[1].height = 30

    ws.merge_cells(start_row=2, start_column=1, end_row=2, end_column=span)
    sub = ws.cell(row=2, column=1, value=subtitle)
    sub.font = SUB_FONT
    sub.alignment = LEFT
    ws.row_dimensions[2].height = 18
    ws.row_dimensions[3].height = 6


def build_main_sheet(wb, models, vendors, group_ratio, rate, api_base, generated_at, cfg):
    ws = wb.active
    ws.title = "模型报价总表"
    ws.sheet_view.showGridLines = False
    multiplier = cfg["multiplier"]
    has_token = any(m.get("quota_type") == 0 for m in models)

    headers = [
        "序号",
        "模型名称",
        "供应商",
        "标签",
        "计费方式",
        "广场展示价",
    ]
    widths = [6, 26, 12, 10, 12, 34]
    if has_token:
        headers += [
            "输入价\n(¥/1M tokens)",
            "输出价\n(¥/1M tokens)",
            "缓存价\n(¥/1M tokens)",
        ]
        widths += [14, 14, 14]
    headers += [
        "单价 (¥)",
        "计费单位",
        "适用分组",
        "接口端点",
        "模型说明",
    ]
    widths += [12, 10, 14, 26, 62]

    write_title(
        ws,
        "模型广场 · 模型名称与报价表",
        f"数据来源：{api_base}/api/pricing（生产快照）　导出时间：{generated_at}　"
        f"展示币种：CNY（1 USD = {rate} CNY）　价格口径：与模型广场卡片展示价一致，取可用分组最低倍率"
        + (
            f"　本次调整：已剔除文本模型 {len(cfg['dropped'])} 个，seedance 系列价格 ×{multiplier:g}"
            if cfg["dropped"] or multiplier != 1.0
            else ""
        ),
        len(headers),
    )
    style_header(ws, 4, headers, widths)

    vendor_name = {v["id"]: v["name"] for v in vendors}
    row = 5
    for idx, model in enumerate(models, start=1):
        name = model.get("model_name", "")
        prices = compute_prices(model, group_ratio, rate, multiplier=multiplier)
        if prices["kind"] == "token":
            billing = "按 Token"
            unit = "1M tokens"
        else:
            billing = "按秒" if prices["per_second"] else "按次"
            unit = "秒" if prices["per_second"] else "次"

        values = [
            (idx, "seq"),
            (name, "name"),
            (vendor_name.get(model.get("vendor_id"), "—"), "center"),
            (model.get("tags") or "—", "center"),
            (billing, "center"),
            (card_summary(model, prices), "mono"),
        ]
        if has_token:
            values += [
                (prices["input"], "token"),
                (prices["output"], "token"),
                (prices["cache"], "token"),
            ]
        values += [
            (prices["fixed"], "fixed"),
            (unit, "center"),
            (", ".join(model.get("enable_groups") or []) or "—", "center"),
            (", ".join(model.get("supported_endpoint_types") or []) or "—", "center"),
            (model.get("description") or "", "text"),
        ]

        for col, (value, kind) in enumerate(values, start=1):
            cell = ws.cell(row=row, column=col, value=value)
            cell.border = BORDER
            cell.font = BODY_FONT
            if idx % 2 == 0:
                cell.fill = BAND_FILL
            if kind == "name":
                cell.font = BODY_BOLD
                cell.alignment = LEFT
            elif kind == "mono":
                cell.font = MONO_FONT
                cell.alignment = LEFT
            elif kind == "token":
                cell.alignment = RIGHT
                cell.number_format = MONEY_TOKEN
            elif kind == "fixed":
                cell.alignment = RIGHT
                cell.number_format = MONEY_UNIT
            elif kind == "center":
                cell.alignment = CENTER
            else:
                cell.alignment = WRAP_LEFT
        desc = model.get("description") or ""
        ws.row_dimensions[row].height = fitted_height(desc, widths[-1])
        row += 1

    last_row = row - 1
    ws.auto_filter.ref = f"A4:{get_column_letter(len(headers))}{last_row}"
    ws.freeze_panes = "C5"
    ws.page_setup.orientation = "landscape"
    ws.page_setup.fitToWidth = 1
    ws.page_setup.fitToHeight = 0
    ws.sheet_properties.pageSetUpPr.fitToPage = True
    ws.print_title_rows = "4:4"

    token_models = [m for m in models if m.get("quota_type") == 0]
    per_call = [m for m in models if m.get("quota_type") == 1 and not is_per_second_model(m.get("model_name", ""))]
    per_second = [m for m in models if m.get("quota_type") == 1 and is_per_second_model(m.get("model_name", ""))]

    note_row = last_row + 2
    ws.merge_cells(start_row=note_row, start_column=1, end_row=note_row, end_column=len(headers))
    parts = []
    if token_models:
        parts.append(f"按 Token 计费 {len(token_models)} 个")
    if per_call:
        parts.append(f"按次计费 {len(per_call)} 个")
    if per_second:
        parts.append(f"按秒计费 {len(per_second)} 个")
    stat = ws.cell(
        row=note_row,
        column=1,
        value=f"合计 {len(models)} 个模型：" + " · ".join(parts),
    )
    stat.font = BODY_BOLD
    stat.alignment = LEFT
    ws.row_dimensions[note_row].height = 22

    ws.merge_cells(start_row=note_row + 1, start_column=1, end_row=note_row + 1, end_column=len(headers))
    tip = ws.cell(
        row=note_row + 1,
        column=1,
        value=(
            "计价公式（与模型广场一致）：按 Token 模型 1M tokens 价 = model_ratio × 2 × 分组倍率；"
            "输出价 = 输入价 × completion_ratio；缓存价 = 输入价 × cache_ratio；"
            "按次/按秒模型单价 = model_price × 分组倍率。美元价统一按 1 USD = "
            f"{rate} CNY 换算为人民币。"
        ),
    )
    tip.font = SUB_FONT
    tip.alignment = LEFT
    ws.row_dimensions[note_row + 1].height = 30

    if cfg["dropped"] or multiplier != 1.0:
        adjust_row = note_row + 2
        ws.merge_cells(start_row=adjust_row, start_column=1, end_row=adjust_row, end_column=len(headers))
        parts = []
        if cfg["dropped"]:
            parts.append(
                "已剔除文本（按 Token 计费）模型：" + "、".join(cfg["dropped"])
            )
        if multiplier != 1.0:
            parts.append(
                "seedance 系列价格按 ×%g 调整：" % multiplier
                + "、".join(cfg["seedance_changes"])
            )
        parts.append("其余模型价格与模型广场一致，未做调整")
        cell = ws.cell(row=adjust_row, column=1, value="；".join(parts))
        cell.font = SUB_FONT
        cell.alignment = LEFT
        ws.row_dimensions[adjust_row].height = 30
    return ws


def build_group_sheet(wb, models, group_ratio, usable_group, rate, cfg):
    ws = wb.create_sheet("分组价格明细")
    ws.sheet_view.showGridLines = False
    multiplier = cfg["multiplier"]

    groups = list(group_ratio.keys())
    headers = ["序号", "模型名称", "计费方式", "计费单位"] + [
        f"{g} 分组价" for g in groups
    ] + [f"{g} 分组倍率" for g in groups]
    widths = [6, 26, 12, 10] + [16] * len(groups) + [12] * len(groups)

    write_title(
        ws,
        "分组价格明细",
        "同一模型在不同分组下的实际价格；分组倍率越低价格越便宜。模型未开通的分组显示「—」。",
        len(headers),
    )
    style_header(ws, 4, headers, widths)

    row = 5
    for idx, model in enumerate(models, start=1):
        name = model.get("model_name", "")
        any_price = compute_prices(model, group_ratio, rate, multiplier=multiplier)
        billing = (
            "按 Token"
            if any_price["kind"] == "token"
            else ("按秒" if any_price["per_second"] else "按次")
        )
        unit = (
            "1M tokens"
            if any_price["kind"] == "token"
            else ("秒" if any_price["per_second"] else "次")
        )
        enabled = set(model.get("enable_groups") or [])

        values = [idx, name, billing, unit]
        for g in groups:
            if g not in enabled:
                values.append("—")
            else:
                p = compute_prices(model, group_ratio, rate, group=g, multiplier=multiplier)
                values.append(
                    p["input"] if p["kind"] == "token" else p["fixed"]
                )
        for g in groups:
            values.append(group_ratio.get(g, 1.0))

        for col, value in enumerate(values, start=1):
            cell = ws.cell(row=row, column=col, value=value)
            cell.border = BORDER
            cell.font = BODY_FONT
            if idx % 2 == 0:
                cell.fill = BAND_FILL
            if col == 2:
                cell.font = BODY_BOLD
                cell.alignment = LEFT
            elif 5 <= col <= 4 + len(groups):
                cell.alignment = RIGHT
                if isinstance(value, (int, float)):
                    cell.number_format = MONEY_TOKEN if any_price["kind"] == "token" else MONEY_UNIT
                else:
                    cell.alignment = CENTER
            elif col > 4 + len(groups):
                cell.alignment = CENTER
                cell.number_format = "0.##"
            else:
                cell.alignment = CENTER
        ws.row_dimensions[row].height = 24
        row += 1

    ws.page_setup.orientation = "landscape"
    ws.page_setup.fitToWidth = 1
    ws.page_setup.fitToHeight = 0
    ws.sheet_properties.pageSetUpPr.fitToPage = True
    ws.print_title_rows = "4:4"

    ws.auto_filter.ref = f"A4:{get_column_letter(len(headers))}{row - 1}"
    ws.freeze_panes = "C5"

    note_row = row + 1
    ws.merge_cells(start_row=note_row, start_column=1, end_row=note_row, end_column=len(headers))
    desc = "、".join(
        f"{g}（{usable_group.get(g, '')}，倍率 {group_ratio[g]}）" for g in groups
    )
    cell = ws.cell(
        row=note_row,
        column=1,
        value=f"分组说明：{desc}。当前广场展示价按模型可用分组中的最低倍率计算。",
    )
    cell.font = SUB_FONT
    cell.alignment = LEFT
    return ws


def build_raw_sheet(wb, models, cfg):
    ws = wb.create_sheet("原始计费参数")
    ws.sheet_view.showGridLines = False
    multiplier = cfg["multiplier"]

    headers = [
        "模型名称",
        "quota_type",
        "model_ratio",
        "model_price",
        "completion_ratio",
        "cache_ratio",
        "create_cache_ratio",
        "image_ratio",
        "audio_ratio",
        "audio_completion_ratio",
        "enable_groups",
        "supported_endpoint_types",
        "vendor_id",
        "tags",
        "pricing_version",
        "本次导出价格系数",
    ]
    widths = [26, 11, 14, 22, 16, 13, 17, 12, 12, 20, 16, 26, 10, 10, 30, 18]

    write_title(
        ws,
        "原始计费参数（对账用）",
        "直接取自 /api/pricing 的原始字段，未做任何换算，便于与后台 ModelRatio / ModelPrice 配置逐项核对。",
        len(headers),
    )
    style_header(ws, 4, headers, widths)

    row = 5
    for idx, model in enumerate(models, start=1):
        values = [
            model.get("model_name", ""),
            model.get("quota_type"),
            model.get("model_ratio"),
            model.get("model_price"),
            model.get("completion_ratio"),
            model.get("cache_ratio"),
            model.get("create_cache_ratio"),
            model.get("image_ratio"),
            model.get("audio_ratio"),
            model.get("audio_completion_ratio"),
            ", ".join(model.get("enable_groups") or []),
            ", ".join(model.get("supported_endpoint_types") or []),
            model.get("vendor_id"),
            model.get("tags") or "",
            model.get("pricing_version") or "",
            seedance_factor(model.get("model_name", ""), multiplier),
        ]
        for col, value in enumerate(values, start=1):
            cell = ws.cell(row=row, column=col, value=value)
            cell.border = BORDER
            cell.font = BODY_FONT
            if idx % 2 == 0:
                cell.fill = BAND_FILL
            if col == 1:
                cell.font = BODY_BOLD
                cell.alignment = LEFT
            elif col in (2, 11, 12, 13, 14, 16):
                cell.alignment = CENTER
                if col == 16:
                    cell.number_format = "0.##"
            elif col in (3, 4, 5, 6, 7, 8, 9, 10):
                cell.alignment = RIGHT
                cell.number_format = "0.##########"
            else:
                cell.alignment = LEFT
                cell.font = MONO_FONT
        ws.row_dimensions[row].height = 22
        row += 1

    ws.auto_filter.ref = f"A4:{get_column_letter(len(headers))}{row - 1}"
    ws.freeze_panes = "B5"
    return ws


def build_notes_sheet(wb, endpoint_map, group_ratio, usable_group, rate, generated_at, api_base, cfg):
    ws = wb.create_sheet("说明与端点")
    ws.sheet_view.showGridLines = False
    ws.column_dimensions["A"].width = 22
    ws.column_dimensions["B"].width = 30
    ws.column_dimensions["C"].width = 62

    write_title(
        ws,
        "说明与接口端点",
        f"快照时间：{generated_at}　数据来源：{api_base}/api/pricing 与 {api_base}/api/status",
        3,
    )

    row = 4
    if cfg["dropped"] or cfg["multiplier"] != 1.0:
        ws.cell(row=row, column=1, value="本次导出调整").font = HEAD_FONT
        ws.cell(row=row, column=1).fill = SECTION_FILL
        ws.merge_cells(start_row=row, start_column=1, end_row=row, end_column=3)
        row += 1
        if cfg["dropped"]:
            ws.cell(row=row, column=1, value="剔除文本模型").font = BODY_BOLD
            ws.cell(row=row, column=1).alignment = WRAP_LEFT
            ws.merge_cells(start_row=row, start_column=2, end_row=row, end_column=3)
            c = ws.cell(
                row=row,
                column=2,
                value="已剔除以下按 Token 计费的对话/文本模型（%d 个）：%s"
                % (len(cfg["dropped"]), "、".join(cfg["dropped"])),
            )
            c.font = BODY_FONT
            c.alignment = WRAP_LEFT
            ws.row_dimensions[row].height = 32
            row += 1
        if cfg["multiplier"] != 1.0:
            ws.cell(row=row, column=1, value="seedance 价格调整").font = BODY_BOLD
            ws.cell(row=row, column=1).alignment = WRAP_LEFT
            ws.merge_cells(start_row=row, start_column=2, end_row=row, end_column=3)
            c = ws.cell(
                row=row,
                column=2,
                value="seedance 系列价格按 ×%g 调整：%s；其余模型价格不变。"
                % (cfg["multiplier"], "、".join(cfg["seedance_changes"])),
            )
            c.font = BODY_FONT
            c.alignment = WRAP_LEFT
            ws.row_dimensions[row].height = 32
            row += 1
        row += 1

    ws.cell(row=row, column=1, value="计价口径").font = BODY_BOLD
    ws.cell(row=row, column=1).fill = SECTION_FILL
    ws.cell(row=row, column=1).font = HEAD_FONT
    ws.merge_cells(start_row=row, start_column=1, end_row=row, end_column=3)
    row += 1
    for label, text in [
        ("按 Token 计费", "quota_type = 0。1M tokens 价 = model_ratio × 2 × 分组倍率（USD），输出价再乘 completion_ratio，缓存价再乘 cache_ratio。"),
        ("按次 / 按秒计费", "quota_type = 1。单次价 = model_price × 分组倍率（USD）；标注「按秒」的模型单价指每秒价格。"),
        ("分组倍率", "取模型可用分组中的最低倍率作为广场展示价；本次快照的分组倍率为 " + "、".join(f"{g}={group_ratio[g]}" for g in group_ratio) + "。"),
        ("币种换算", f"美元价按 1 USD = {rate} CNY 换算成人民币（取自 /api/status 的 usd_exchange_rate）。"),
        ("数据性质", "本表为生产环境 /api/pricing 的静态快照，不含任何密钥与凭据。"),    ]:
        ws.cell(row=row, column=1, value=label).font = BODY_BOLD
        ws.cell(row=row, column=1).alignment = WRAP_LEFT
        ws.merge_cells(start_row=row, start_column=2, end_row=row, end_column=3)
        c = ws.cell(row=row, column=2, value=text)
        c.font = BODY_FONT
        c.alignment = WRAP_LEFT
        ws.row_dimensions[row].height = 32
        row += 1

    row += 1
    ws.cell(row=row, column=1, value="接口端点").font = HEAD_FONT
    ws.cell(row=row, column=1).fill = SECTION_FILL
    ws.merge_cells(start_row=row, start_column=1, end_row=row, end_column=3)
    row += 1
    for head, col in (("端点类型", 1), ("请求路径", 2), ("方法", 3)):
        c = ws.cell(row=row, column=col, value=head)
        c.font = HEAD_FONT
        c.fill = HEAD_FILL
        c.alignment = CENTER
        c.border = BORDER
    row += 1
    for name, info in endpoint_map.items():
        ws.cell(row=row, column=1, value=name).font = BODY_FONT
        ws.cell(row=row, column=2, value=info.get("path", "")).font = MONO_FONT
        ws.cell(row=row, column=3, value=info.get("method", "")).font = BODY_FONT
        for col in (1, 2, 3):
            ws.cell(row=row, column=col).border = BORDER
            ws.cell(row=row, column=col).alignment = LEFT
        row += 1

    row += 1
    ws.cell(row=row, column=1, value="分组").font = HEAD_FONT
    ws.cell(row=row, column=1).fill = SECTION_FILL
    ws.merge_cells(start_row=row, start_column=1, end_row=row, end_column=3)
    row += 1
    for head, col in (("分组", 1), ("倍率", 2), ("说明", 3)):
        c = ws.cell(row=row, column=col, value=head)
        c.font = HEAD_FONT
        c.fill = HEAD_FILL
        c.alignment = CENTER
        c.border = BORDER
    row += 1
    for g, ratio in group_ratio.items():
        ws.cell(row=row, column=1, value=g).font = BODY_FONT
        ws.cell(row=row, column=2, value=ratio).font = BODY_FONT
        ws.cell(row=row, column=2).number_format = "0.##"
        ws.cell(row=row, column=3, value=usable_group.get(g, "")).font = BODY_FONT
        for col in (1, 2, 3):
            ws.cell(row=row, column=col).border = BORDER
            ws.cell(row=row, column=col).alignment = LEFT
        row += 1
    return ws


# --- main --------------------------------------------------------------------
def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--api", default="https://api.heibaidao.cn")
    parser.add_argument(
        "--out",
        default=os.path.join(
            os.path.expanduser("~"), "Downloads", "模型广场报价表.xlsx"
        ),
    )
    parser.add_argument(
        "--include-text",
        action="store_true",
        help="保留按 Token 计费的文本/对话模型（默认剔除）",
    )
    parser.add_argument(
        "--seedance-multiplier",
        type=float,
        default=2.0,
        help="seedance 系列价格倍数，默认 2（传 1 表示不调整）",
    )
    args = parser.parse_args()

    api_base = args.api.rstrip("/")
    status_raw = fetch_json(f"{api_base}/api/status")
    status = status_raw.get("data") or status_raw
    pricing = fetch_json(f"{api_base}/api/pricing")

    rate = float(status.get("usd_exchange_rate") or status.get("price") or 1)
    display_type = status.get("quota_display_type") or "USD"
    models = pricing.get("data") or []
    vendors = pricing.get("vendors") or []
    group_ratio = pricing.get("group_ratio") or {"default": 1.0}
    usable_group = pricing.get("usable_group") or {}
    endpoint_map = pricing.get("supported_endpoint") or {}

    dropped = (
        []
        if args.include_text
        else [m.get("model_name", "") for m in models if is_text_model(m)]
    )
    kept = [m for m in models if args.include_text or not is_text_model(m)]

    seedance_changes = []
    for m in kept:
        name = m.get("model_name", "")
        if "seedance" not in name.lower() or args.seedance_multiplier == 1.0:
            continue
        before = compute_prices(m, group_ratio, rate, multiplier=1.0)
        after = compute_prices(m, group_ratio, rate, multiplier=args.seedance_multiplier)
        if before["kind"] == "token":
            seedance_changes.append(
                "%s 输入 %s→%s/1M"
                % (name, money(before["input"], 4, 6), money(after["input"], 4, 6))
            )
        else:
            unit = "秒" if before["per_second"] else "次"
            seedance_changes.append(
                "%s %s→%s/%s"
                % (
                    name,
                    money(before["fixed"], 4, 4),
                    money(after["fixed"], 4, 4),
                    unit,
                )
            )

    cfg = {
        "multiplier": args.seedance_multiplier,
        "dropped": dropped,
        "seedance_changes": seedance_changes,
    }

    generated_at = dt.datetime.now().strftime("%Y-%m-%d %H:%M:%S")
    snapshot_dir = os.path.join(
        os.path.dirname(os.path.dirname(os.path.abspath(__file__))),
        ".workbuddy-ai",
        "tmp",
    )
    os.makedirs(snapshot_dir, exist_ok=True)
    with open(
        os.path.join(snapshot_dir, "pricing-snapshot.json"), "w", encoding="utf-8"
    ) as fp:
        json.dump(
            {"status": status, "pricing": pricing, "exported_at": generated_at},
            fp,
            ensure_ascii=False,
            indent=2,
        )

    wb = Workbook()
    build_main_sheet(wb, kept, vendors, group_ratio, rate, api_base, generated_at, cfg)
    build_group_sheet(wb, kept, group_ratio, usable_group, rate, cfg)
    build_raw_sheet(wb, kept, cfg)
    build_notes_sheet(
        wb, endpoint_map, group_ratio, usable_group, rate, generated_at, api_base, cfg
    )

    out_path = os.path.abspath(args.out)
    os.makedirs(os.path.dirname(out_path), exist_ok=True)
    try:
        wb.save(out_path)
    except PermissionError:
        # 目标文件正被 Excel 占用：先落临时文件，再尝试原子替换，失败则写新文件名。
        tmp_path = out_path + ".tmp.xlsx"
        wb.save(tmp_path)
        try:
            os.replace(tmp_path, out_path)
        except PermissionError:
            base, ext = os.path.splitext(out_path)
            out_path = base + "-更新" + ext
            os.replace(tmp_path, out_path)
            print("WARN target file is locked by another program, wrote a new file instead")
    except IsADirectoryError:
        raise

    print("OK models=%d (dropped_text=%d) vendors=%d rate=%s display=%s seedance_x%s"
          % (len(kept), len(dropped), len(vendors), rate, display_type, args.seedance_multiplier))
    print("OUT %s" % out_path)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
