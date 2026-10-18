// Regression test for the dialog focus trap.
//
// Every caller passes a fresh onClose closure, so the dialog's focus effect
// used to tear down and re-run on each render of the page that owns it. A
// keystroke re-renders that page, so the caret was dragged to the dialog's
// first focusable element after every character: forms could not be filled in.
//
// The assertion that matters is where focus ends up after typing, not merely
// that the value changed - a plain controlled input passes a value-only check
// while still being unusable.

import { useState } from "react";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it } from "vitest";
import { Field, Input, Modal } from "./ui";

// vitest runs without globals here, so testing-library's automatic cleanup is
// not registered for us. Without this, each render() leaves its tree in the
// document and the next test sees duplicate elements.
afterEach(cleanup);

// Mirrors how pages use the dialog: the page owns both the draft state and the
// onClose closure, so typing re-renders the page and hands the dialog a new
// onClose every keystroke.
function Page({ withField = true }: { withField?: boolean }) {
  const [open, setOpen] = useState(false);
  const [draft, setDraft] = useState("");
  return (
    <div>
      <button type="button" onClick={() => setOpen(true)}>
        Add provider
      </button>
      <Modal
        open={open}
        title="Add provider"
        onClose={() => setOpen(false)}
        footer={<button type="button">Save</button>}
      >
        {withField ? (
          <Field label="Name" hint="Shown in traces.">
            <Input value={draft} onChange={(e) => setDraft(e.target.value)} />
          </Field>
        ) : (
          <p>Nothing to fill in here.</p>
        )}
      </Modal>
    </div>
  );
}

describe("Modal focus handling", () => {
  it("keeps focus in the field while the operator types", async () => {
    const user = userEvent.setup();
    render(<Page />);

    await user.click(screen.getByRole("button", { name: "Add provider" }));
    const field = screen.getByRole("textbox");

    // Opening a form should put the caret in it, not on the close button.
    expect(document.activeElement).toBe(field);

    await user.keyboard("openai-primary");

    expect((field as HTMLInputElement).value).toBe("openai-primary");
    expect(document.activeElement).toBe(field);
  });

  it("returns focus to the trigger when the dialog closes", async () => {
    const user = userEvent.setup();
    render(<Page />);

    const trigger = screen.getByRole("button", { name: "Add provider" });
    await user.click(trigger);
    expect(screen.getByRole("dialog")).toBeTruthy();

    await user.keyboard("{Escape}");

    expect(screen.queryByRole("dialog")).toBeNull();
    expect(document.activeElement).toBe(trigger);
  });

  it("still focuses the dialog when it has no fields", async () => {
    const user = userEvent.setup();
    render(<Page withField={false} />);

    await user.click(screen.getByRole("button", { name: "Add provider" }));

    const dialog = screen.getByRole("dialog");
    expect(dialog.contains(document.activeElement)).toBe(true);
  });
});
