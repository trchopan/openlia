import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { JournalPreviewPane } from "./JournalPreviewPane";

describe("JournalPreviewPane", () => {
  const sampleJournal = `; Sample Payslip
2020-08-01 Payslip 2020-08
    assets:cash                                  20618182 VND
    expenses:tax:pit                              2290909 VND
    income:salary:payslip                       -22909091 VND
`;

  it("renders Sankey diagram and highlight cards", () => {
    render(
      <JournalPreviewPane
        content={sampleJournal}
        filePath="finance/journal.hledger"
      />,
    );

    // Sankey section exists
    expect(
      screen.getByRole("region", { name: "Sankey Diagram Flow" }),
    ).toBeInTheDocument();
    expect(screen.getByText("Money Flow (Sankey Diagram)")).toBeInTheDocument();

    // Highlights section exists
    expect(
      screen.getByRole("region", { name: "Journal Highlights" }),
    ).toBeInTheDocument();
    expect(screen.getByText("TOTAL INCOME")).toBeInTheDocument();
    expect(screen.getByText("TOTAL EXPENSES")).toBeInTheDocument();
    expect(screen.getByText("NET RETAINED")).toBeInTheDocument();

    // Transactions breakdown exists
    expect(screen.getByText("Transactions (1)")).toBeInTheDocument();
    expect(screen.getByText("Payslip 2020-08")).toBeInTheDocument();
    expect(screen.getByText("assets:cash")).toBeInTheDocument();
    expect(
      screen.queryByRole("tablist", { name: "Journal currencies" }),
    ).not.toBeInTheDocument();
  });

  it("handles empty journal gracefully", () => {
    render(
      <JournalPreviewPane
        content="; only comments here"
        filePath="finance/journal.hledger"
      />,
    );

    expect(
      screen.getByText("No journal transactions detected"),
    ).toBeInTheDocument();
  });

  it("switches all journal views to a selected currency", () => {
    const multiCurrencyJournal = `2026-10-09 * Banh Mi
    expenses:food:dining                  15,000 VND
    assets:momo:wallet                   -15,000 VND

2026-10-09 * Claude
    expenses:technology:subscriptions      45.50 USD
    liabilities:credit_cards:visa         -45.50 USD

2026-10-08 * JetBrains
    expenses:technology:software           12.00 USD
    liabilities:credit_cards:visa         -12.00 USD

2026-10-07 * Cloudflare
    expenses:technology:hosting             8.99 USD
    liabilities:credit_cards:visa          -8.99 USD
`;

    render(
      <JournalPreviewPane
        content={multiCurrencyJournal}
        filePath="finance/journal.hledger"
      />,
    );

    expect(
      screen.getByRole("tablist", { name: "Journal currencies" }),
    ).toBeInTheDocument();
    expect(screen.getByText("$66.49")).toBeInTheDocument();
    expect(screen.getAllByText("15,000 ₫").length).toBeGreaterThan(0);

    fireEvent.click(screen.getByRole("tab", { name: /^USD$/ }));

    expect(screen.getAllByText("$66.49").length).toBeGreaterThan(0);
    expect(screen.getByText("Transactions (3)")).toBeInTheDocument();
    expect(screen.queryByText("expenses:food:dining")).not.toBeInTheDocument();
    expect(screen.queryByText("15,000 ₫")).not.toBeInTheDocument();
  });
});
