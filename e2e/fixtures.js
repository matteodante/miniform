const { test: base, expect } = require("@playwright/test");
const { TestClient } = require("./test-client");

const test = base.extend({
  client: async ({ page }, use) => {
    const client = new TestClient(page);
    try {
      await use(client);
    } finally {
      await client.close();
    }
  },
  admin: async ({ client }, use) => {
    const credentials = await client.operatorCredentials();
    await client.login();
    try {
      await use(client);
    } finally {
      await client.restoreOperatorCredentials(credentials);
    }
  },
});

module.exports = { expect, test };
