// Package contract defines the JSON bodies Depsilo's HTTP API returns. Handlers
// build these types instead of ad hoc gin.H maps, and `make generate` turns this
// package into web/src/lib/generated/apiContract.ts, so the browser's types
// cannot drift from what the server sends.
//
// Rules, enforced by rules_test.go:
//   - Depend only on the standard library. A domain or database type is copied
//     into a contract type at the handler, so the API shape changes only here.
//   - A pointer field is either omitted when nil (`json:",omitempty"`, which
//     TypeScript sees as `field?: T`) or sent as null and declared with
//     `tstype:"T | null,required"`.
//   - time.Time is sent as an RFC 3339 string. "Never" is null, not the zero
//     time: declare the field nullable and fill it with NullableTime.
//   - A tstype contains no comma: tygo splits the tag at commas, so write
//     `{ [K in A]?: B }` rather than `Partial<Record<A, B>>`.
//
// A string-valued enumeration is a named string type whose constants are named
// <Type><Value> in one const block of at least two; it becomes a TypeScript
// union. A field with a single possible value is a plain string with a literal
// tstype such as `tstype:"'deleted'"`, because tygo turns a one-constant block
// into a bare string alias. Error bodies keep the shared {code, message}
// envelope and are not modelled here.
package contract
