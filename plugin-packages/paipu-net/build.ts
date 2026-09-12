import { readFileSync, writeFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { strToU8, zipSync } from "../../web/node_modules/fflate/lib/node.cjs";

const directory = dirname(fileURLToPath(import.meta.url));
const manifestPath = resolve(directory, "manifest.json");
const interfacePath = resolve(directory, "docs/interface.md");
const start = "<!-- YINGCE_MANIFEST_CONTRACT_START -->";
const end = "<!-- YINGCE_MANIFEST_CONTRACT_END -->";
const placeholder = "<当前插件的完整 documentation，由 README.md 与 docs/interface.md 拼接而成；为避免 JSON 递归，此处不重复展开正文。>";
const manifest = JSON.parse(readFileSync(manifestPath, "utf8"));
const readme = readFileSync(resolve(directory, "README.md"), "utf8").replace(/\r\n/g, "\n").trim();
const interfaceText = readFileSync(interfacePath, "utf8").replace(/\r\n/g, "\n");
const marker = interfaceText.indexOf(start);
const prose = (marker < 0 ? interfaceText : interfaceText.slice(0, marker)).trim();
const contract = JSON.stringify({ ...manifest, documentation: placeholder }, null, 2);
const documentation = `${prose}\n\n${start}\n## Manifest 完整接口定义\n\n\`\`\`json\n${contract}\n\`\`\`\n${end}\n`;
manifest.documentation = `${readme}\n\n---\n\n${documentation.trim()}`;
const encoded = `${JSON.stringify(manifest, null, 2)}\n`;
writeFileSync(manifestPath, encoded);
writeFileSync(interfacePath, documentation);
const archive = zipSync({
    "manifest.json": strToU8(encoded),
    "README.md": strToU8(`${readme}\n`),
    "docs/interface.md": strToU8(documentation),
}, { level: 9, mtime: new Date("2026-01-01T00:00:00Z") });
const output = resolve(directory, "../paipu-net.yingce-plugin");
writeFileSync(output, archive);
console.log(`Built ${manifest.id} ${manifest.version}: ${archive.length} bytes`);
