import * as TW from "./index";
import RNAnimated from "react-native-reanimated";

const IS_WEB = process.env.EXPO_OS === "web";

export const Animated = {
  ...RNAnimated,
  View: IS_WEB ? RNAnimated.View : RNAnimated.createAnimatedComponent(TW.View),
  Text: IS_WEB ? RNAnimated.Text : RNAnimated.createAnimatedComponent(TW.Text),
  ScrollView: IS_WEB
    ? RNAnimated.ScrollView
    : RNAnimated.createAnimatedComponent(TW.ScrollView),
};
