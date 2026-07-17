const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const os = require("node:os");
const path = require("node:path");
const { test } = require("node:test");
const { removeOwnedDataDirectory } = require("./global-teardown");

test("global teardown preserves explicit data directories", async (t) => {
  const directory = await fs.mkdtemp(path.join(os.tmpdir(), "miniform-e2e-explicit-"));
  t.after(() => fs.rm(directory, { force: true, recursive: true }));

  await removeOwnedDataDirectory({ MINIFORM_E2E_DATA_DIR: directory });

  await fs.access(directory);
});

test("global teardown removes owned data directories", async () => {
  const directory = await fs.mkdtemp(path.join(os.tmpdir(), "miniform-e2e-owned-"));
  const marker = path.join(directory, ".miniform-e2e-owned");
  await fs.writeFile(marker, "");

  await removeOwnedDataDirectory({
    MINIFORM_E2E_DATA_DIR: directory,
    MINIFORM_E2E_OWNERSHIP_MARKER: marker,
  });

  await assert.rejects(fs.access(directory), { code: "ENOENT" });
});
