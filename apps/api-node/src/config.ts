import dotenv from "dotenv";
import path from "node:path";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const repoRoot = path.resolve(__dirname, "..", "..");
dotenv.config({ path: path.join(repoRoot, ".env") });
dotenv.config({ path: path.join(__dirname, "..", ".env") });

export type AppConfig = {
  addr: string;
  databaseUrl: string;
  authBaseUrl: string;
  adminEmail: string;
  stripeSecretKey: string;
  stripeWebhookSecret: string;
  stripeSuccessUrl: string;
  stripeCancelUrl: string;
};

export function loadConfig(): AppConfig {
  const addr = process.env.API_ADDR?.trim() || ":8788";
  return {
    addr,
    databaseUrl: process.env.DATABASE_URL?.trim() || "",
    authBaseUrl: process.env.AUTH_BASE_URL?.trim() || "",
    adminEmail: process.env.ADMIN_EMAIL?.trim() || "",
    stripeSecretKey: process.env.STRIPE_SECRET_KEY?.trim() || "",
    stripeWebhookSecret: process.env.STRIPE_WEBHOOK_SECRET?.trim() || "",
    stripeSuccessUrl: process.env.STRIPE_SUCCESS_URL?.trim() || "",
    stripeCancelUrl: process.env.STRIPE_CANCEL_URL?.trim() || "",
  };
}
