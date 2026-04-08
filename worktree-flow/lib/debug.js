const fs = require('fs');
const os = require('os');
const path = require('path');

const DEBUG_LOG = path.join(os.tmpdir(), 'wt-debug.log');

function make_debug(tag) {
  return function debug(msg) {
    if (process.env.WT_DEBUG !== '1') return;
    const ts = new Date().toTimeString().slice(0, 12);
    try { fs.appendFileSync(DEBUG_LOG, `[${ts}] [${tag}] ${msg}\n`); } catch {}
  };
}

module.exports = { make_debug, DEBUG_LOG };
