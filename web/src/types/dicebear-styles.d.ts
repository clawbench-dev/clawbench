// Ambient declarations for DiceBear style JSON subpath imports.
//
// The style definitions are large JSON files (up to ~360 KB) shipped per-style
// via the `@dicebear/styles` package exports map (`@dicebear/styles/<style>.json`).
// Without this shim, importing them would either fail (tsconfig has no
// `resolveJsonModule`) or force TypeScript to infer a huge literal type from the
// JSON contents. Declaring the wildcard as `StyleDefinition` keeps the imports
// typed and cheap.
declare module '@dicebear/styles/*.json' {
  const definition: import('@dicebear/core').StyleDefinition
  export default definition
}
