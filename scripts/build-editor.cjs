const fs = require('node:fs');
const path = require('node:path');
const esbuild = require('esbuild');
const root = path.join(__dirname, '..');

esbuild.buildSync({
  absWorkingDir: root,
  entryPoints: ['web/editor-source.js'],
  bundle: true,
  minify: true,
  format: 'iife',
  outfile: 'web/editor-bundle.js',
});

// Keep upstream license notices beside the vendored bundle, including libraries
// without license comments in their distributed JavaScript.
const lock = JSON.parse(fs.readFileSync(path.join(root, 'package-lock.json'), 'utf8'));
const notices = [];
for (const [relative, info] of Object.entries(lock.packages)) {
  if (!relative.startsWith('node_modules/') || info.dev) continue;
  const directory = path.join(root, relative);
  const file = fs.readdirSync(directory).find((name) => /^license(?:\..+)?$/i.test(name));
  if (!file) throw new Error(`Missing license for ${relative}`);
  notices.push(`${relative.replace('node_modules/', '')} ${info.version}\n\n${fs.readFileSync(path.join(directory, file), 'utf8').trim()}`);
}
fs.writeFileSync(path.join(root, 'web/editor-bundle.js.LEGAL.txt'), notices.join('\n\n----------------------------------------\n\n') + '\n');
console.log('Editor bundle and third-party license notices generated.');
