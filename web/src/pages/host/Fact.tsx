import type { ReactNode } from "react";

/** Fact is a label over its value, in a description list: a field of a Host or a Service, or an Attribute. */
export function Fact({ label, className = "", children }: { label: string; className?: string; children: ReactNode }) {
  return (
    <div className={`flex min-w-0 flex-col gap-0.5 ${className}`}>
      <dt className="t-label">{label}</dt>
      <dd className="min-w-0 font-medium break-words">{children}</dd>
    </div>
  );
}
