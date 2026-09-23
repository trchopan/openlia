import { Buffer } from "node:buffer";
import { createHash, randomBytes } from "node:crypto";
import { readFileSync } from "node:fs";

const sessionCookie = "openlia_workspace_session";
const idleTimeoutMs = 30 * 60 * 1000;
const absoluteTimeoutMs = 8 * 60 * 60 * 1000;
const attemptWindowMs = 60 * 1000;
const blockDurationMs = 30 * 1000;
const maxFailures = 5;
const maxAttemptEntries = 10_000;
const maxSessionEntries = 1_024;

interface Session {
  createdAt: number;
  lastSeenAt: number;
}

interface Attempt {
  failures: number;
  firstFailureAt: number;
  blockedUntil: number;
}

export interface AuthenticatorOptions {
  required: boolean;
  passwordHash?: string | undefined;
  passwordHashFile?: string | undefined;
  secureCookies?: boolean | undefined;
}

export type LoginResult =
  | { kind: "ok"; cookie: string }
  | { kind: "invalid" }
  | { kind: "rate_limited" };

export class Authenticator {
  readonly required: boolean;
  readonly secureCookies: boolean;
  private readonly passwordHash: string;
  private readonly sessions = new Map<string, Session>();
  private readonly attempts = new Map<string, Attempt>();
  private verificationInProgress = false;

  constructor(options: AuthenticatorOptions) {
    this.required = options.required;
    this.secureCookies = options.secureCookies ?? false;
    this.passwordHash =
      options.passwordHash ??
      (options.passwordHashFile
        ? readFileSync(options.passwordHashFile, "utf8").trim()
        : "");
    if (this.required && !isSupportedArgon2idHash(this.passwordHash)) {
      throw new Error(
        "workspace-ui password verifier is missing or unsupported",
      );
    }
  }

  sessionResponse(request: Request) {
    return {
      schema: 1 as const,
      auth_required: this.required,
      authenticated: !this.required || this.isAuthenticated(request),
    };
  }

  isAuthenticated(request: Request): boolean {
    if (!this.required) return true;
    this.pruneSessions(Date.now());
    const token = readCookie(request.headers.get("cookie"));
    if (!token) return false;
    const key = digestToken(token);
    const session = this.sessions.get(key);
    if (!session) return false;
    const now = Date.now();
    if (
      now - session.lastSeenAt > idleTimeoutMs ||
      now - session.createdAt > absoluteTimeoutMs
    ) {
      this.sessions.delete(key);
      return false;
    }
    session.lastSeenAt = now;
    return true;
  }

  async login(
    password: string,
    clientKey: string,
    secureCookies = this.secureCookies,
  ): Promise<LoginResult> {
    if (!this.required) return { kind: "ok", cookie: "" };
    const now = Date.now();
    let attempt = this.attempts.get(clientKey);
    if (attempt && attempt.blockedUntil > now) return { kind: "rate_limited" };
    if (attempt && now - attempt.firstFailureAt >= attemptWindowMs) {
      this.attempts.delete(clientKey);
      attempt = undefined;
    }
    if (this.verificationInProgress) return { kind: "rate_limited" };
    this.verificationInProgress = true;
    let valid = false;
    try {
      valid = await Bun.password.verify(
        password,
        this.passwordHash,
        "argon2id",
      );
    } catch {
      valid = false;
    } finally {
      this.verificationInProgress = false;
    }
    if (!valid) {
      this.recordFailure(clientKey, now);
      const blockedUntil = this.attempts.get(clientKey)?.blockedUntil ?? 0;
      return {
        kind: blockedUntil > now ? "rate_limited" : "invalid",
      };
    }
    this.attempts.delete(clientKey);
    const token = randomBytes(32).toString("base64url");
    if (this.sessions.size >= maxSessionEntries) {
      const oldest = this.sessions.keys().next().value;
      if (oldest) this.sessions.delete(oldest);
    }
    this.sessions.set(digestToken(token), { createdAt: now, lastSeenAt: now });
    this.pruneSessions(now);
    return { kind: "ok", cookie: this.cookie(token, secureCookies) };
  }

  logout(request: Request, secureCookies = this.secureCookies): string {
    const token = readCookie(request.headers.get("cookie"));
    if (token) this.sessions.delete(digestToken(token));
    return this.clearCookie(secureCookies);
  }

  private recordFailure(clientKey: string, now: number): void {
    let attempt = this.attempts.get(clientKey);
    if (!attempt || now - attempt.firstFailureAt >= attemptWindowMs) {
      attempt = { failures: 0, firstFailureAt: now, blockedUntil: 0 };
      this.attempts.set(clientKey, attempt);
    }
    attempt.failures += 1;
    if (attempt.failures >= maxFailures)
      attempt.blockedUntil = now + blockDurationMs;
    if (this.attempts.size > maxAttemptEntries) {
      const oldest = this.attempts.keys().next().value;
      if (oldest) this.attempts.delete(oldest);
    }
  }

  private pruneSessions(now: number): void {
    for (const [key, session] of this.sessions) {
      if (
        now - session.lastSeenAt > idleTimeoutMs ||
        now - session.createdAt > absoluteTimeoutMs
      )
        this.sessions.delete(key);
    }
  }

  private cookie(token: string, secureCookies: boolean): string {
    const secure = secureCookies ? "; Secure" : "";
    return `${sessionCookie}=${token}; Path=/; HttpOnly; SameSite=Strict; Max-Age=${absoluteTimeoutMs / 1000}${secure}`;
  }

  private clearCookie(secureCookies: boolean): string {
    const secure = secureCookies ? "; Secure" : "";
    return `${sessionCookie}=; Path=/; HttpOnly; SameSite=Strict; Max-Age=0${secure}`;
  }
}

function digestToken(token: string): string {
  return createHash("sha256").update(token).digest("hex");
}

function readCookie(header: string | null): string | undefined {
  if (!header) return undefined;
  for (const item of header.split(";")) {
    const [name, ...value] = item.trim().split("=");
    if (name === sessionCookie) return value.join("=") || undefined;
  }
  return undefined;
}

function isSupportedArgon2idHash(value: string): boolean {
  const parts = value.split("$");
  if (
    parts.length !== 6 ||
    parts[0] !== "" ||
    parts[1] !== "argon2id" ||
    parts[2] !== "v=19"
  )
    return false;
  const params = new Map(
    (parts[3] ?? "").split(",").map((item) => {
      const [key, value] = item.split("=");
      return [key ?? "", value ?? ""] as const;
    }),
  );
  const memory = Number(params.get("m"));
  const time = Number(params.get("t"));
  const parallelism = Number(params.get("p"));
  const salt = Buffer.from(parts[4] ?? "", "base64");
  const key = Buffer.from(parts[5] ?? "", "base64");
  return (
    Number.isInteger(memory) &&
    memory >= 32 * 1024 &&
    memory <= 128 * 1024 &&
    Number.isInteger(time) &&
    time >= 2 &&
    time <= 5 &&
    Number.isInteger(parallelism) &&
    parallelism >= 1 &&
    parallelism <= 4 &&
    salt.length >= 16 &&
    key.length === 32
  );
}

export { sessionCookie };
