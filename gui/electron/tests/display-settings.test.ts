import test from "node:test";
import assert from "node:assert/strict";
import {
  defaultDisplaySettings,
  isDisplaySettings,
  zoomFontSize,
} from "../src/shared/display-settings";

test("display settings validate persisted bounds and font text", () => {
  assert.equal(isDisplaySettings(defaultDisplaySettings), true);
  assert.equal(
    isDisplaySettings({ fontSize: 20, fontFamily: "Courier New" }),
    true,
  );
  for (const fontSize of [7, 41, 14.5, NaN, "14", undefined]) {
    assert.equal(
      isDisplaySettings({ ...defaultDisplaySettings, fontSize }),
      false,
    );
  }
  for (const fontFamily of [
    "",
    " ",
    "x".repeat(257),
    "font\nname",
    "x; color:red",
    undefined,
  ]) {
    assert.equal(
      isDisplaySettings({ ...defaultDisplaySettings, fontFamily }),
      false,
    );
  }
  assert.equal(
    isDisplaySettings({ ...defaultDisplaySettings, extra: true }),
    false,
  );
  assert.equal(zoomFontSize(14, -100), 15);
  assert.equal(zoomFontSize(14, 100), 13);
  assert.equal(zoomFontSize(8, 1), 8);
  assert.equal(zoomFontSize(40, -1), 40);
  assert.equal(zoomFontSize(14, 0), 14);
});
