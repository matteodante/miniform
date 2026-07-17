const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { defineConfig, devices } = require("@playwright/test");

const projectRoot = path.resolve(__dirname, "..");
const port = Number(process.env.PLAYWRIGHT_TEST_PORT || 41817);
const baseURL = process.env.PLAYWRIGHT_BASE_URL || `http://127.0.0.1:${port}`;
const serverCommand = process.env.MINIFORM_E2E_SERVER_COMMAND || "go run ./cmd/miniform";
const browserPath = process.env.PLAYWRIGHT_BROWSERS_PATH || path.join(projectRoot, "tmp", "ms-playwright");
const dataRoot = process.env.MINIFORM_DATA_DIR
  ? path.resolve(projectRoot, process.env.MINIFORM_DATA_DIR)
  : os.tmpdir();
fs.mkdirSync(dataRoot, { recursive: true });
const ownsDataDirectory = !process.env.MINIFORM_E2E_DATA_DIR;
const dataDirectory = process.env.MINIFORM_E2E_DATA_DIR
  || fs.mkdtempSync(path.join(dataRoot, "miniform-e2e-"));
const ownershipMarker = path.join(dataDirectory, ".miniform-e2e-owned");
if (ownsDataDirectory) fs.writeFileSync(ownershipMarker, "", { flag: "wx" });
fs.mkdirSync(browserPath, { recursive: true });
process.env.PLAYWRIGHT_BROWSERS_PATH = browserPath;
process.env.MINIFORM_DATA_DIR = dataDirectory;
process.env.MINIFORM_E2E_DATA_DIR = dataDirectory;
process.env.MINIFORM_E2E_OWNERSHIP_MARKER = ownsDataDirectory ? ownershipMarker : "";

module.exports = defineConfig({
  testDir: __dirname,
  testMatch: "**/*.spec.js",
  workers: 1,
  forbidOnly: Boolean(process.env.CI),
  retries: process.env.CI ? 1 : 0,
  reporter: process.env.CI ? "github" : "line",
  globalTeardown: require.resolve("./global-teardown"),
  timeout: 30_000,
  expect: { timeout: 5_000 },
  use: {
    baseURL,
    actionTimeout: 8_000,
    navigationTimeout: 15_000,
    screenshot: "only-on-failure",
    trace: process.env.CI ? "retain-on-failure" : "off",
    extraHTTPHeaders: { "X-Test-Source": "playwright" },
  },
  projects: [{
    name: "chromium",
    use: { ...devices["Desktop Chrome"], reducedMotion: "reduce" },
  }],
  webServer: {
    cwd: projectRoot,
    command: serverCommand,
    env: {
      ...process.env,
      GOCACHE: path.join(projectRoot, "tmp", "go-cache"),
      LOG_LEVEL: "error",
      MINIFORM_ENV: "test",
      MINIFORM_PORT: String(port),
    },
    url: `${baseURL}/_health`,
    reuseExistingServer: false,
    timeout: 60_000,
    gracefulShutdown: { signal: "SIGTERM", timeout: 2_000 },
  },
});
