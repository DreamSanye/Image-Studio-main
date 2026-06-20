import assert from "node:assert/strict";
import test from "node:test";

const profiles = await import("../src/lib/profiles.ts");

function makeProfile(name) {
  return {
    id: name,
    name,
    apiMode: "responses",
    requestPolicy: "openai",
    baseURL: "",
    textModelID: "",
    imageModelID: "",
    reasoningEffort: "xhigh",
    concurrencyLimit: 0,
    createdAt: 1,
  };
}

test("default profile names start from 配置1 even when 主配置 exists", () => {
  assert.equal(profiles.nextDefaultProfileName([makeProfile("主配置")]), "配置1");
});

test("default profile names use the first available numeric slot", () => {
  assert.equal(
    profiles.nextDefaultProfileName([
      makeProfile("主配置"),
      makeProfile("配置1"),
      makeProfile("配置3"),
    ]),
    "配置2",
  );
});

test("blank profiles use sequential default names", () => {
  const existing = [makeProfile("配置1")];
  assert.equal(profiles.makeBlankProfile("images", existing).name, "配置2");
});

test("blank responses profile defaults reasoning effort to xhigh", () => {
  assert.equal(profiles.makeBlankProfile("responses").reasoningEffort, "xhigh");
});

test("legacy profiles without reasoningEffort normalize to xhigh", () => {
  const parsed = profiles.tryParseProfile({
    id: "p1",
    name: "配置1",
    apiMode: "responses",
    requestPolicy: "openai",
    baseURL: "",
    textModelID: "",
    imageModelID: "",
    concurrencyLimit: 0,
    createdAt: 1,
  });
  assert.equal(parsed?.reasoningEffort, "xhigh");
});

test("legacy profiles preserve fallbackProfileId when present", () => {
  const parsed = profiles.tryParseProfile({
    id: "p1",
    name: "配置1",
    apiMode: "responses",
    requestPolicy: "openai",
    baseURL: "",
    textModelID: "",
    imageModelID: "",
    reasoningEffort: "xhigh",
    concurrencyLimit: 0,
    fallbackProfileId: "backup-profile",
    createdAt: 1,
  });
  assert.equal(parsed?.fallbackProfileId, "backup-profile");
});
