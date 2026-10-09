import { render, screen } from "@testing-library/react";
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
});
