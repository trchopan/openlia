#!/usr/bin/env python3
"""Summarize normalized hledger account totals without changing source files."""

from __future__ import annotations

import argparse
import json
import sys
from decimal import Decimal, InvalidOperation
from pathlib import Path
from typing import Any


GROUPS = ("income", "expenses", "assets", "liabilities")


def _amount(value: Any, label: str) -> Decimal:
    if isinstance(value, bool) or value is None:
        raise ValueError(f"{label} must be a finite number")
    try:
        amount = Decimal(str(value))
    except (InvalidOperation, ValueError) as exc:
        raise ValueError(f"{label} must be a finite number") from exc
    if not amount.is_finite():
        raise ValueError(f"{label} must be a finite number")
    return amount


def _rows(data: dict[str, Any], group: str) -> list[dict[str, Any]]:
    values = data.get(group, [])
    if not isinstance(values, list) or not all(isinstance(value, dict) for value in values):
        raise ValueError(f"{group} must be a list of objects")
    rows: list[dict[str, Any]] = []
    for index, value in enumerate(values, start=1):
        account = str(value.get("account") or "").strip()
        if not account:
            raise ValueError(f"{group} row {index} must have an account")
        rows.append({"account": account, "amount": _amount(value.get("amount"), f"{group} row {index} amount")})
    rows.sort(key=lambda row: row["account"].casefold())
    return rows


def _total(rows: list[dict[str, Any]]) -> Decimal:
    return sum((row["amount"] for row in rows), Decimal("0"))


def _format(amount: Decimal) -> str:
    return format(amount, "f")


def _notes(data: dict[str, Any]) -> list[str]:
    values = data.get("notes", [])
    if not isinstance(values, list):
        raise ValueError("notes must be a list")
    return [str(value) for value in values]


def build_review(data: dict[str, Any]) -> dict[str, Any]:
    if not isinstance(data, dict):
        raise ValueError("input must be a JSON object")
    rows = {group: _rows(data, group) for group in GROUPS}
    totals = {group: _total(rows[group]) for group in GROUPS}
    income = -totals["income"]
    expenses = totals["expenses"]
    return {
        "period": str(data.get("period") or "unspecified"),
        "currency": str(data.get("currency") or "unspecified"),
        "income_total": _format(income),
        "expense_total": _format(expenses),
        "net_income": _format(income - expenses),
        "assets_total": _format(totals["assets"]),
        "liabilities_total": _format(totals["liabilities"]),
        "net_worth": _format(totals["assets"] + totals["liabilities"]),
        "accounts": {
            group: [{"account": row["account"], "amount": _format(row["amount"])} for row in rows[group]]
            for group in GROUPS
        },
        "notes": _notes(data),
        "method": "Income and expense totals are derived from normalized hledger account totals; no journal is read or changed.",
    }


def render_markdown(review: dict[str, Any]) -> str:
    lines = [
        "# Personal Finance Review",
        "",
        f"Period: {review['period']}",
        f"Currency: {review['currency']}",
        "",
        "## Summary",
        "",
        f"- Income: {review['income_total']}",
        f"- Expenses: {review['expense_total']}",
        f"- Net income: {review['net_income']}",
        f"- Assets: {review['assets_total']}",
        f"- Liabilities: {review['liabilities_total']}",
        f"- Net worth: {review['net_worth']}",
        "",
        "## Account Totals",
        "",
    ]
    for group in GROUPS:
        lines.append(f"### {group.title()}")
        lines.extend(f"- {row['account']}: {row['amount']}" for row in review["accounts"][group])
        if not review["accounts"][group]:
            lines.append("- None supplied.")
        lines.append("")
    lines.extend(["## Notes", ""])
    lines.extend(f"- {note}" for note in review["notes"] or ["None supplied."])
    lines.extend(["", "## Method", "", review["method"], ""])
    return "\n".join(lines)


def self_test() -> None:
    review = build_review(
        {
            "period": "2026-08",
            "currency": "USD",
            "income": [{"account": "income:salary", "amount": "-5000"}],
            "expenses": [
                {"account": "expenses:rent", "amount": "1500"},
                {"account": "expenses:food", "amount": "250"},
            ],
            "assets": [{"account": "assets:checking", "amount": "5000"}],
            "liabilities": [{"account": "liabilities:card", "amount": "-200"}],
        }
    )
    assert review["income_total"] == "5000"
    assert review["expense_total"] == "1750"
    assert review["net_income"] == "3250"
    assert review["net_worth"] == "4800"
    assert "Personal Finance Review" in render_markdown(review)


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("input", nargs="?", type=Path, help="JSON normalized hledger totals")
    parser.add_argument("--format", choices=("json", "markdown"), default="json")
    parser.add_argument("--output", type=Path, help="Write the report to a file")
    parser.add_argument("--self-test", action="store_true", help="Run the built-in deterministic test")
    args = parser.parse_args(argv)
    if args.self_test:
        self_test()
        print("ok")
        return 0
    if args.input is None:
        parser.error("input is required unless --self-test is used")
    try:
        review = build_review(json.loads(args.input.read_text(encoding="utf-8")))
        rendered = (
            json.dumps(review, indent=2, sort_keys=True, ensure_ascii=True) + "\n"
            if args.format == "json"
            else render_markdown(review)
        )
        if args.output:
            args.output.write_text(rendered, encoding="utf-8")
        else:
            sys.stdout.write(rendered)
    except (OSError, ValueError, json.JSONDecodeError) as exc:
        print(f"error: {exc}", file=sys.stderr)
        return 2
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
