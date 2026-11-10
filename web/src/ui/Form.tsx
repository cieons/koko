import type { FormEvent, ReactNode } from "react";

/** FormProps are what a form holds and does. */
export interface FormProps {
  /** onSubmit sends the form; it runs only once every field is valid. */
  onSubmit: () => void;
  /** label names the form, such as its dialog's title. */
  label?: string;
  className?: string;
  children: ReactNode;
}

/**
 * Form is a form that checks its fields when it is submitted
 * (docs/web-design.md, "States every screen has"). Its submit button stays
 * enabled; on submit, each field at fault shows its error under it, the
 * first takes the focus, and the form is not sent. It carries noValidate, so
 * the browser's own bubbles never show: the fields' React Aria validation
 * hears the invalid events checkValidity fires and shows koko's messages.
 */
export function Form({ onSubmit, label, className, children }: FormProps) {
  const submit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (event.currentTarget.checkValidity()) {
      onSubmit();
    }
  };
  return (
    <form noValidate aria-label={label} className={className} onSubmit={submit}>
      {children}
    </form>
  );
}
