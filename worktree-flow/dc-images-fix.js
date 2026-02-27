const { execSync } = require('child_process');
const config_mod = require('./config');
const config = config_mod.load_config({ required: false }) || null;

function find_mongo_container() {
  try {
    const output = execSync('docker ps --format "{{.Names}}"', {
      stdio: 'pipe',
      encoding: 'utf8',
    }).trim();
    const names = output.split('\n').filter(Boolean);
    const project_name = config ? config.name : 'project';
    return names.find((n) => n.includes(project_name) && n.includes('mongo')) || null;
  } catch {
    return null;
  }
}

function main() {
  if (config && !config_mod.feature_enabled(config, 'imagesFix')) {
    console.log('Image URL fixing is not enabled for this project.');
    return;
  }

  const args = process.argv.slice(2);
  const default_db = config ? config.database.defaultDb : 'db';
  const db_name = args.find((a) => a.startsWith('--db='))?.split('=')[1] || default_db;
  const dry_run = args.includes('--dry-run');

  const container = find_mongo_container();
  if (!container) {
    console.error('Could not find MongoDB container.');
    process.exit(1);
  }

  const mongo_script = `
    db = db.getSiblingDB("${db_name}");
    var total = 0;
    var dry = ${dry_run};
    var re = /^https?:\\/\\/[^/]+\\/fakes3\\//;

    function fix(url) {
      return typeof url === "string" ? url.replace(re, "/fakes3/") : url;
    }

    function fix_string(coll, field) {
      var f = {}; f[field] = { $regex: "^https?://[^/]+/fakes3/" };
      var count = db[coll].countDocuments(f);
      if (!count) return;
      if (dry) { print("  [dry-run] " + coll + "." + field + ": " + count); total += count; return; }
      db[coll].find(f).forEach(function(doc) {
        var val = doc; field.split(".").forEach(function(p) { val = val[p]; });
        if (typeof val !== "string") return;
        var s = {}; s[field] = fix(val);
        db[coll].updateOne({ _id: doc._id }, { $set: s });
      });
      var after = db[coll].countDocuments(f);
      print("  " + coll + "." + field + ": " + (count - after) + " updated");
      total += count - after;
    }

    function fix_array(coll, field) {
      var f = {}; f[field] = { $regex: "^https?://[^/]+/fakes3/" };
      var count = db[coll].countDocuments(f);
      if (!count) return;
      if (dry) { print("  [dry-run] " + coll + "." + field + ": " + count); total += count; return; }
      db[coll].find(f).forEach(function(doc) {
        var s = {}; s[field] = (doc[field] || []).map(fix);
        db[coll].updateOne({ _id: doc._id }, { $set: s });
      });
      var after = db[coll].countDocuments(f);
      print("  " + coll + "." + field + ": " + (count - after) + " updated");
      total += count - after;
    }

    function fix_nested(coll, parent, child) {
      var path = parent + "." + child;
      var f = {}; f[path] = { $regex: "^https?://[^/]+/fakes3/" };
      var count = db[coll].countDocuments(f);
      if (!count) return;
      if (dry) { print("  [dry-run] " + coll + "." + path + ": " + count); total += count; return; }
      db[coll].find(f).forEach(function(doc) {
        (doc[parent] || []).forEach(function(elem) {
          if (elem && typeof elem[child] === "string") elem[child] = fix(elem[child]);
          if (elem && Array.isArray(elem[child])) elem[child] = elem[child].map(fix);
        });
        var s = {}; s[parent] = doc[parent];
        db[coll].updateOne({ _id: doc._id }, { $set: s });
      });
      var after = db[coll].countDocuments(f);
      print("  " + coll + "." + path + ": " + (count - after) + " updated");
      total += count - after;
    }

    print("Fixing absolute fakes3 URLs in " + "${db_name}" + "...\\n");

    fix_string("items", "image");
    fix_array("items", "images");
    fix_string("listings", "image");
    fix_array("listings", "images");
    fix_nested("listings", "variants", "image");
    fix_nested("listings", "variants", "images");
    fix_string("kits", "image");
    fix_array("kits", "images");
    fix_string("liquid_templates", "thumbnail_url");
    fix_nested("accounts", "users", "avatar");
    fix_nested("accounts", "stores", "logo");
    fix_string("accounts", "sales_portal.logo");

    print("");
    if (dry) print("Dry run complete. " + total + " records would be updated.");
    else print("Done. " + total + " records updated.");
  `;

  console.log(`Using MongoDB container: ${container}`);
  console.log(`Database: ${db_name}`);
  if (dry_run) console.log('Mode: dry-run\n');
  else console.log('');

  try {
    execSync(
      `docker exec ${container} mongo --quiet --eval '${mongo_script.replace(/'/g, "'\\''")}'`,
      { stdio: 'inherit' },
    );
  } catch {
    console.error('Failed to run MongoDB script.');
    process.exit(1);
  }
}

main();
