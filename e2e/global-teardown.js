const fs = require("node:fs/promises");
const path = require("node:path");

async function removeOwnedDataDirectory(environment = process.env) {
  const dataDirectory = environment.MINIFORM_E2E_DATA_DIR;
  const ownershipMarker = environment.MINIFORM_E2E_OWNERSHIP_MARKER;
  if (!dataDirectory || !ownershipMarker) return;

  try {
    const markerStat = await fs.lstat(ownershipMarker);
    if (!markerStat.isFile()) return;

    const [directory, marker] = await Promise.all([
      fs.realpath(dataDirectory),
      fs.realpath(ownershipMarker),
    ]);
    if (marker !== path.join(directory, ".miniform-e2e-owned")) return;
    await fs.rm(directory, { force: true, recursive: true, maxRetries: 3, retryDelay: 100 });
  } catch (error) {
    if (error.code !== "ENOENT") throw error;
  }
}

module.exports = async function globalTeardown() {
  await removeOwnedDataDirectory();
};

module.exports.removeOwnedDataDirectory = removeOwnedDataDirectory;
