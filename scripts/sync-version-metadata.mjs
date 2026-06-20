import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const productVersion = process.env.IMAGE_STUDIO_PRODUCT_VERSION;
const frontendVersion = process.env.IMAGE_STUDIO_FRONTEND_VERSION ?? productVersion;

if (!productVersion) {
  throw new Error("IMAGE_STUDIO_PRODUCT_VERSION is required");
}
if (!frontendVersion) {
  throw new Error("IMAGE_STUDIO_FRONTEND_VERSION is required");
}

const updates = [
  {
    path: path.join(root, "image-studio", "wails.json"),
    mutate(json) {
      json.info.productVersion = productVersion;
    },
  },
  {
    path: path.join(root, "image-studio", "frontend", "package.json"),
    mutate(json) {
      json.version = frontendVersion;
    },
  },
  {
    path: path.join(root, "image-studio", "frontend", "package-lock.json"),
    mutate(json) {
      json.version = frontendVersion;
      if (json.packages?.[""]) {
        json.packages[""].version = frontendVersion;
      }
    },
  },
];

for (const update of updates) {
  const json = JSON.parse(fs.readFileSync(update.path, "utf8"));
  update.mutate(json);
  fs.writeFileSync(update.path, `${JSON.stringify(json, null, 2)}\n`);
}
