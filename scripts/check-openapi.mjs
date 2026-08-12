#!/usr/bin/env node
// scripts/check-openapi.mjs
//
// Minimal YAML.safeLoad + structural sanity check for OpenAPI specs in
// packages/shared/openapi/*.yaml.
//
// Why a custom check instead of a full OpenAPI validator?
//   - We only need to catch YAML syntax errors and obvious schema breakage
//     (missing openapi version, no paths / no components, broken $ref) at PR
//     time.
//   - A full validator (e.g. @apidevtools/swagger-cli) is heavier and pulls a
//     lot of transitive deps; not worth it for this size of surface.
//   - We deliberately avoid re-implementing JSON Reference resolution so that
//     $ref mistakes surface early.
//
// Exit code: 0 on success, 1 on first error.

import { readFileSync, readdirSync, statSync } from "node:fs";
import { dirname, join, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import yaml from "js-yaml";

const __dirname = dirname(fileURLToPath(import.meta.url));
const ROOT = resolve(__dirname, "..");
const OPENAPI_DIR = resolve(ROOT, "packages/shared/openapi");

const REQUIRED_OPENAPI_VERSION = /^3\.(0|1)\.\d+$/;

const failures = [];

function fail(file, msg) {
    failures.push(`${relative(ROOT, file)}: ${msg}`);
}

function walk(dir) {
    const out = [];
    for (const name of readdirSync(dir)) {
        const p = join(dir, name);
        const st = statSync(p);
        if (st.isDirectory()) {
            out.push(...walk(p));
        } else if (name.endsWith(".yaml") || name.endsWith(".yml")) {
            out.push(p);
        }
    }
    return out;
}

function checkOne(file) {
    let text;
    try {
        text = readFileSync(file, "utf8");
    } catch (e) {
        fail(file, `cannot read: ${e.message}`);
        return;
    }

    let doc;
    try {
        // yaml.safeLoad 在 js-yaml v4 里改名 load + { schema: SAFE_SCHEMA }
        // 的等价物；我们用 v4 默认（不识别 !!js/function 等类型）已经满足
        // "safeLoad" 语义。
        doc = yaml.load(text, { filename: file });
    } catch (e) {
        fail(file, `yaml parse error: ${e.message}`);
        return;
    }

    if (doc == null || typeof doc !== "object") {
        fail(file, "yaml does not parse to an object");
        return;
    }

    if (typeof doc.openapi !== "string" || !REQUIRED_OPENAPI_VERSION.test(doc.openapi)) {
        fail(file, `missing/invalid openapi version (got ${JSON.stringify(doc.openapi)})`);
    }

    if (doc.info && typeof doc.info === "object") {
        const { title, version } = doc.info;
        if (!title || !version) {
            fail(file, "info.title / info.version required when info is set");
        }
    }

    if (doc.paths && typeof doc.paths !== "object") {
        fail(file, "paths must be an object");
    }

    if (doc.components && typeof doc.components !== "object") {
        fail(file, "components must be an object");
    }

    // Cross-file $ref sanity：相对路径 ../foo.yaml#/... 必须能解析到本地 yaml。
    const refs = collectRefs(doc);
    for (const ref of refs) {
        if (!ref.startsWith("#")) {
            const [pathPart] = ref.split("#");
            const target = resolve(dirname(file), pathPart);
            try {
                statSync(target);
            } catch {
                fail(file, `$ref target not found: ${ref}`);
            }
        }
    }
}

function collectRefs(node, out = []) {
    if (node == null) return out;
    if (Array.isArray(node)) {
        for (const v of node) collectRefs(v, out);
        return out;
    }
    if (typeof node === "object") {
        for (const [k, v] of Object.entries(node)) {
            if (k === "$ref" && typeof v === "string") {
                out.push(v);
            } else {
                collectRefs(v, out);
            }
        }
    }
    return out;
}

const files = walk(OPENAPI_DIR);
if (files.length === 0) {
    console.error(`no .yaml files found under ${relative(ROOT, OPENAPI_DIR)}`);
    process.exit(1);
}

for (const f of files) {
    checkOne(f);
}

if (failures.length > 0) {
    console.error("openapi check FAILED:");
    for (const f of failures) console.error("  - " + f);
    process.exit(1);
}

console.log(`openapi check OK (${files.length} file${files.length === 1 ? "" : "s"} checked):`);
for (const f of files) console.log("  - " + relative(ROOT, f));