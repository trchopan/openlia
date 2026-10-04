---
name: personal-finance
description: Use hledger to organize, validate, and analyze personal finance journals, spending, budgets, and reconciliations.
version: 0.1.0
platforms: [linux]
required_environment_variables: []
required_credential_files: []
metadata:
  hermes:
    tags: [personal-os, finance, hledger, accounting]
    category: finance
---

# Personal Finance

## When to Use

Use when personal finance information needs to be recorded, checked, imported,
reconciled, or analyzed with hledger. This includes spending reviews, budgets,
cash flow, balances, and net-worth snapshots.

## Data Model

Keep the source journal and any hledger rules files in the runtime workspace,
normally under `/opt/data/workspace/finance/`. Do not put bank exports,
credentials, account numbers, or other personal records in the tracked profile
or skill files. Prefer one main journal that includes smaller files when the
ledger is large enough to benefit from that organization.

Use conventional account groups when they fit the person's records:

- `assets:` for cash, bank, savings, and investments.
- `liabilities:` for credit cards, loans, and other obligations.
- `equity:` for opening balances and other equity entries.
- `income:` for salary, benefits, interest, and other inflows.
- `expenses:` for spending categories.

Account names and currencies are user choices. Do not rename or reclassify
existing accounts without explaining the proposed change first.

## Procedure

1. Identify the journal. Use `LEDGER_FILE` when it is set. Otherwise inspect the
   runtime workspace's `finance/` directory and use its single, unambiguous
   existing hledger journal. If there are multiple plausible journals, ask
   which one to use. Never expose the complete journal in a response unless it
   is specifically requested.
2. If no journal exists, do not create one for a read-only request. When the
   person explicitly approves a specific journal change, such as recording a
   proposed transaction, create `finance/journal.hledger` in the runtime
   workspace as part of that approved change. Do not ask for a journal path just
   to perform this setup; tell the person which default path you used. Preserve
   any existing file at that path, and ask if it is present but cannot safely be
   identified as the journal.
3. For a balanced transaction, use a payment/source account the person has
   stated or configured as their default. Do not infer `assets:cash` or another
   balancing account just from wording such as “I spent”. If the source account
   is unknown, ask which account was used before recording the transaction.
4. Validate before analyzing with `hledger -f JOURNAL check`. If validation
   fails, report the error and stop rather than producing financial conclusions
   from an invalid ledger.
5. Run only the reports needed for the question. Common read-only commands are:
   `stats`, `balance`, `register`, `incomestatement`, `balancesheet`, and
   `cashflow`. Add an explicit period or account query when appropriate, and
   record the journal path, date range, account scope, and currencies used.
6. For a structured review, normalize the relevant hledger account totals into
   JSON for `scripts/finance_review.py`. Its `income` values use hledger's
   normal sign convention, where income balances are usually negative and
   expense balances are usually positive. Treat the helper as a presentation
   aid, not as a replacement for hledger validation.
7. Separate reported facts from interpretation. Call out pending or uncleared
   entries, omitted accounts, incomplete periods, missing prices, and any
   assumptions about currency conversion or account classification.
8. For CSV or other imports, preserve the original export, inspect the columns
   and rules, preview the converted postings with hledger, and check for
   duplicates before proposing an import. Do not run `hledger import`,
   `hledger add`, or edit a journal until the person explicitly approves the
   exact change.
9. After an approved journal change, run `hledger check` again and show a
   concise diff or transaction summary. Never send money, place a trade, or
   change an external financial account as part of this workflow.

## Reports

Choose reports based on the question rather than presenting every available
number:

- Spending: query `expenses:` with `balance` or `register`, normally bounded by
  a requested period.
- Income and savings: use `incomestatement` and explain the sign convention
  before calculating a savings result.
- Cash and debt: use `balancesheet` or targeted `balance` queries for assets and
  liabilities.
- Cash movement: use `cashflow` and distinguish transfers from income or
  expenses.
- Reconciliation: compare the relevant account and date range with the source
  statement, and preserve balance assertions when they are available.

## Pitfalls

- A balanced transaction is not proof that its payee, account, date, or amount
  is correct.
- Do not treat a transfer between two accounts as income or spending.
- Do not hide pending, uncleared, or asserted-balance discrepancies.
- Do not combine currencies or value investments without explicit prices,
  valuation dates, and a stated conversion method.
- Do not use forecasts, periodic transactions, or budgets as actual historical
  results.
- Do not import the same bank statement twice; use stable source identifiers or
  a reviewed date/payee/amount comparison.
- hledger output is accounting data, not automatically tax, legal, or investment
  advice. State when professional advice or source documentation is needed.

## Verification

Confirm the journal passes `hledger check`, every reported total has a source
command and scope, currencies and date boundaries are visible, and the source
files remained unchanged during read-only analysis. For approved writes,
confirm the post-change check passes and no unrelated transactions changed.
