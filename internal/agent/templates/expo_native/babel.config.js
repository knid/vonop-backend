module.exports = function (api) {
  api.cache(true);
  return {
    presets: [
      ["babel-preset-expo", { jsxImportSource: "nativewind" }],
      "nativewind/babel",
    ],
    plugins: [
      // react-native-worklets/plugin MUST be listed last per Reanimated 4 docs.
      "react-native-worklets/plugin",
    ],
  };
};
