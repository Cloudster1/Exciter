// Generates src/api/schema.d.ts from api/openapi.json, or with --check fails
// when the committed copy no longer matches the document.
import { execFileSync } from "node:child_process";
import { existsSync, mkdtempSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

const web = fileURLToPath(new URL("..", import.meta.url));
const documentPath = join(web, "..", "api", "openapi.json");
const schemaPath = join(web, "src", "api", "schema.d.ts");
const generator = join(web, "node_modules", ".bin", "openapi-typescript");

function generate(from, to) {
  execFileSync(generator, [from, "-o", to], { stdio: ["ignore", "ignore", "inherit"] });
}

if (!existsSync(documentPath)) {
  console.error("api/openapi.json is missing. Run `make openapi` to write it from the route table.");
  process.exit(1);
}

if (!process.argv.includes("--check")) {
  generate(documentPath, schemaPath);
  console.log("schema.d.ts generated from api/openapi.json");
  process.exit(0);
}

const scratch = mkdtempSync(join(tmpdir(), "exciter-schema-"));
try {
  const fresh = join(scratch, "schema.d.ts");
  generate(documentPath, fresh);
  const committed = existsSync(schemaPath) ? readFileSync(schemaPath, "utf8") : "";
  if (readFileSync(fresh, "utf8") !== committed) {
    console.error("web/src/api/schema.d.ts is out of date with api/openapi.json. Run `make web-schema` and commit the result.");
    process.exit(1);
  }
  console.log("schema.d.ts matches api/openapi.json");
} finally {
  rmSync(scratch, { recursive: true, force: true });
}
