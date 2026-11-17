import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { QuestionForm, formAnswer, formIntro, parseForm, parseFormAnswer } from "../src/components/agent-chat/QuestionForm";

const encoded = [
  `form:${JSON.stringify({ text: "Which gate?", options: ["A [RECOMMENDED]", "B"] })}`,
  `form:${JSON.stringify({ text: "Name?", options: [] })}`,
  `form:${JSON.stringify({ text: "Where?", options: ["here", "there"] })}`,
];

describe("question form", () => {
  it("reads sub-questions only from form-encoded options, and the intro before the numbered list", () => {
    expect(parseForm(encoded)?.map((q) => q.text)).toEqual(["Which gate?", "Name?", "Where?"]);
    expect(parseForm(["A", "B"])).toBeUndefined();
    expect(formIntro("Before planning.\n\n1. Which gate?\n2. Name?")).toBe("Before planning.");
    expect(formIntro("1. Which gate?\n2. Name?")).toBe("");
  });

  it("asks one question at a time, moves on when answered, and goes back to change an answer", () => {
    render(<QuestionForm questions={parseForm(encoded)!} onSubmit={vi.fn()} />);
    expect(screen.getByTestId("question-title").textContent).toBe("Which gate?");
    expect(screen.getByText("Recommended")).toBeTruthy(); // the mark is a tag, not part of the option's words
    fireEvent.click(screen.getByRole("button", { name: /^2\s*B/ }));
    expect(screen.getByTestId("question-title").textContent).toBe("Name?");
    fireEvent.click(screen.getByLabelText("Previous question"));
    expect(screen.getByTestId("question-title").textContent).toBe("Which gate?");
    expect(screen.getByRole("button", { name: /^2\s*B/ }).getAttribute("aria-pressed")).toBe("true");
  });

  it("sends only after every question is answered or skipped, with a skipped one marked", () => {
    const onSubmit = vi.fn();
    render(<QuestionForm questions={parseForm(encoded)!} onSubmit={onSubmit} />);
    fireEvent.click(screen.getByRole("button", { name: /^2\s*B/ }));              // 1 answered
    fireEvent.change(screen.getByLabelText("answer 2"), { target: { value: "Ada" } });
    fireEvent.click(screen.getByTestId("question-next"));                           // 2 typed
    fireEvent.click(screen.getByTestId("question-skip"));                           // 3 skipped
    const send = screen.getByTestId("question-send") as HTMLButtonElement;
    expect(send.disabled).toBe(false);
    fireEvent.click(send);
    expect(onSubmit).toHaveBeenCalledWith("1. Which gate?\n   → B\n2. Name?\n   → Ada\n3. Where?\n   → (skipped)");
  });

  it("cannot send while a question is still unanswered; a review row takes you back to it", () => {
    const onSubmit = vi.fn();
    render(<QuestionForm questions={parseForm(encoded)!} onSubmit={onSubmit} />);
    fireEvent.click(screen.getByLabelText("Next question")); // not allowed: nothing answered yet
    expect(screen.getByTestId("question-title").textContent).toBe("Which gate?");
    fireEvent.click(screen.getByTestId("question-skip"));
    fireEvent.click(screen.getByTestId("question-skip"));
    fireEvent.click(screen.getByTestId("question-skip"));
    // all three skipped: review shows it, and sending is allowed
    expect((screen.getByTestId("question-send") as HTMLButtonElement).disabled).toBe(false);
    fireEvent.click(within3("Which gate?"));
    expect(screen.getByTestId("question-title").textContent).toBe("Which gate?");
  });

  it("reads a sent answer back into one row per question", () => {
    const text = formAnswer(parseForm(encoded)!, ["B", "Ada", null]);
    const rows = parseFormAnswer(text)!;
    expect(rows.map((r) => [r.n, r.answer, r.skipped])).toEqual([[1, "B", false], [2, "Ada", false], [3, "(skipped)", true]]);
    expect(parseFormAnswer("just one plain answer")).toBeUndefined();
  });
});

function within3(question: string): HTMLElement {
  return screen.getByText(question, { selector: "span" }).closest("button")!;
}
