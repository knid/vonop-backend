// NativeWind v4 babel setup (canonical):
//
//   - `babel-preset-expo` with `jsxImportSource: "nativewind"` — rewrites JSX to
//     use NativeWind's runtime so className props are picked up on every component.
//   - `nativewind/babel` as a PRESET (not a plugin — v3 used `plugins`, v4 moved
//     it to presets; passing it as `plugins` throws ".plugins is not a valid
//     Plugin property" at Metro bundle time).
module.exports = function (api) {
  api.cache(true);
  return {
    presets: [
      ["babel-preset-expo", { jsxImportSource: "nativewind" }],
      "nativewind/babel",
    ],
  };
};
