// Prepends the provenance header required for generated files.
import { readFileSync, writeFileSync } from "node:fs";

const file = process.argv[2];
const header = "// Generated from api/openapi.json by `npm --prefix web run generate:api`. Do not edit.\n";
const body = readFileSync(file, "utf8");
writeFileSync(file, body.startsWith(header) ? body : header + body);
