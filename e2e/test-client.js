const crypto = require("node:crypto");
const path = require("node:path");
const sqlite3 = require("sqlite3");

const ADMIN_EMAIL = "admin@miniform.local";
const ADMIN_PASSWORD = "miniform";

class TestClient {
  constructor(page) {
    this.page = page;
    this.database = null;
  }

  async open(pathname) {
    const response = await this.page.goto(pathname, { waitUntil: "domcontentloaded" });
    if (!response || !response.ok()) {
      throw new Error(`GET ${pathname} returned ${response?.status() ?? "no response"}`);
    }
    return response;
  }

  async login(email = ADMIN_EMAIL, password = ADMIN_PASSWORD) {
    await this.open("/admin/login");
    await this.page.getByLabel("Email").fill(email);
    await this.page.getByLabel("Password").fill(password);
    await Promise.all([
      this.page.waitForURL((url) => !url.pathname.endsWith("/admin/login")),
      this.page.getByRole("button", { name: "Open inbox" }).click(),
    ]);
  }

  async logout() {
    await Promise.all([
      this.page.waitForURL(/\/admin\/login/),
      this.page.getByRole("button", { name: "Sign out" }).click(),
    ]);
  }

  async submit(slug, token, fields) {
    return this.page.request.post(`/forms/${slug}/submit?token=${token}`, {
      form: fields,
      headers: { Origin: "http://localhost:3000" },
    });
  }

  async createMailer(name) {
    const now = databaseTimestamp();
    const result = await this.run(
      `INSERT INTO mailer_profiles
       (name, default_from_name, default_from_email, smtp_host, smtp_port, smtp_encryption, created_at, updated_at)
       VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
      [name, "Miniform", "no-reply@example.com", "smtp.example.com", 587, "starttls", now, now],
    );
    return result.lastID;
  }

  async createCaptcha(name, { siteKey = "test-site-key", secret = "test-secret" } = {}) {
    const now = databaseTimestamp();
    const result = await this.run(
      `INSERT INTO captcha_profiles
       (name, site_key, secret_key, created_at, updated_at)
       VALUES (?, ?, ?, ?, ?)`,
      [name, siteKey, secret, now, now],
    );
    return result.lastID;
  }

  async createForm(name, slug, options = {}) {
    const now = databaseTimestamp();
    const token = crypto.randomBytes(24).toString("hex");
    const publicID = crypto.randomBytes(10).toString("hex");
    const result = await this.run(
      `INSERT INTO forms
       (public_id, name, slug, token, allowed_origins, uploads_enabled, captcha_profile_id, created_at, updated_at)
       VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
      [
        publicID, name, slug, token, options.allowedOrigins ?? "*",
        options.uploadsEnabled ? 1 : 0, options.captchaProfileID ?? null, now, now,
      ],
    );

    await this.run(
      `INSERT INTO email_deliveries
       (form_id, mailer_profile_id, enabled, recipient, created_at, updated_at)
       VALUES (?, ?, ?, ?, ?, ?)`,
      [
        result.lastID,
        options.email?.mailerProfileID ?? null,
        options.email ? 1 : 0,
        options.email?.recipient ?? "",
        now,
        now,
      ],
    );
    await this.run(
      `INSERT INTO webhook_deliveries (form_id, enabled, url, created_at, updated_at)
       VALUES (?, ?, ?, ?, ?)`,
      [result.lastID, options.webhookURL ? 1 : 0, options.webhookURL ?? "", now, now],
    );
    return { id: result.lastID, slug, token };
  }

  async operatorCredentials() {
    return this.row("SELECT id, email, password_hash FROM users ORDER BY id LIMIT 1");
  }

  async restoreOperatorCredentials(credentials) {
    if (!credentials) return;
    await this.run(
      "UPDATE users SET email = ?, password_hash = ? WHERE id = ?",
      [credentials.email, credentials.password_hash, credentials.id],
    );
  }

  async row(sql, parameters = []) {
    const database = await this.connect();
    return new Promise((resolve, reject) => {
      database.get(sql, parameters, (error, row) => (error ? reject(error) : resolve(row)));
    });
  }

  async run(sql, parameters = []) {
    const database = await this.connect();
    return new Promise((resolve, reject) => {
      database.run(sql, parameters, function callback(error) {
        if (error) reject(error);
        else resolve({ lastID: this.lastID, changes: this.changes });
      });
    });
  }

  async connect() {
    if (this.database) return this.database;
    const projectRoot = path.resolve(__dirname, "..");
    const configuredDirectory = process.env.MINIFORM_DATA_DIR || "storage";
    const dataDirectory = path.isAbsolute(configuredDirectory)
      ? configuredDirectory
      : path.join(projectRoot, configuredDirectory);
    const filename = process.env.MINIFORM_DATABASE_FILENAME || "miniform.test.db";
    const databasePath = path.isAbsolute(filename) ? filename : path.join(dataDirectory, filename);

    this.database = await new Promise((resolve, reject) => {
      const database = new sqlite3.Database(databasePath, (error) => (error ? reject(error) : resolve(database)));
    });
    this.database.configure("busyTimeout", 5000);
    return this.database;
  }

  async close() {
    if (!this.database) return;
    const database = this.database;
    this.database = null;
    await new Promise((resolve, reject) => database.close((error) => (error ? reject(error) : resolve())));
  }
}

function databaseTimestamp() {
  return new Date().toISOString().replace("T", " ").replace("Z", "+00:00");
}

function uniqueName(prefix) {
  return `${prefix}-${Date.now()}-${crypto.randomBytes(3).toString("hex")}`;
}

module.exports = { ADMIN_EMAIL, ADMIN_PASSWORD, TestClient, uniqueName };
