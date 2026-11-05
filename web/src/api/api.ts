// Code generated from internal/shared/api/api.go by make generate; DO NOT EDIT.

/** Op is one API operation, with its request and response types. */
export interface Op<Req, Resp> {
  /** The operation's name, "<noun>.<verb>", such as "space.add". */
  readonly name: string;
  /** The operation is a POST to this path, such as /v1/space.add. */
  readonly path: string;
  /** A write may change the record; a read never does. */
  readonly write: boolean;
  /** Never set: it carries the request and response types to a client. */
  readonly types?: { request: Req; response: Resp };
}
