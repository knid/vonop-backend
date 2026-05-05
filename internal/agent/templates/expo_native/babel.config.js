// NativeWind v4 babel setup (canonical):
//
//   - `babel-preset-expo` with `jsxImportSource: "nativewind"` — rewrites JSX to
//     use NativeWind's runtime so className props are picked up on every component.
//   - `nativewind/babel` as a PRESET (not a plugin — v3 used `plugins`, v4 moved
//     it to presets; passing it as `plugins` throws ".plugins is not a valid
//     Plugin property" at Metro bundle time).
//   - `stripImportMeta` (inline) — zustand v5's ESM middleware (`zustand/esm/
//     middleware.mjs`, picked by Metro for web) ships `import.meta.env`
//     references inside devtools.js. Metro's web profile (hermes-stable) wraps
//     each module in a function but does NOT transform `import.meta` — V8 then
//     throws "Cannot use 'import.meta' outside a module" the moment the bundle
//     loads, so React never mounts (blank white preview). The published
//     `babel-plugin-transform-import-meta` only rewrites `import.meta.url|
//     filename|dirname|resolve`, so we strip the meta-property entirely.
//     `import.meta.env?.MODE` then evaluates to `undefined`, which the
//     try/catch around zustand's devtools detection already handles. Native
//     Hermes builds aren't affected — only `expo start --web`.
const stripImportMeta = () => ({
  name: "strip-import-meta",
  visitor: {
    MetaProperty(path) {
      const { meta, property } = path.node;
      if (meta && meta.name === "import" && property && property.name === "meta") {
        path.replaceWith({ type: "ObjectExpression", properties: [] });
      }
    },
  },
});

module.exports = function (api) {
  api.cache(true);
  return {
    presets: [
      ["babel-preset-expo", { jsxImportSource: "nativewind" }],
      "nativewind/babel",
    ],
    plugins: [stripImportMeta],
  };
};
