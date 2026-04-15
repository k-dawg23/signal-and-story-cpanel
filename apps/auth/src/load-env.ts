import dotenv from "dotenv";
import path from "node:path";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
// apps/auth/src -> monorepo root is apps/auth/src/../../
const repoRoot = path.resolve(__dirname, "..", "..");

dotenv.config({ path: path.join(repoRoot, ".env") });
dotenv.config({ path: path.join(__dirname, "..", ".env") });
