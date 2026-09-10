#!/usr/bin/env node
// compress-gltf.cjs
// 用 gltf-pipeline 对 GLB/GLTF 进行 Draco 压缩（KHR_draco_mesh_compression）
// 用法示例：
// node compress-gltf.cjs --in r.glb --out r.draco.glb --cl 10 --qpos 14 --qnor 10 --quv 12 --qgen 12 --unified
// 支持 .glb / .gltf 输入；输出建议 .glb

const fs = require('fs');
const path = require('path');
const { processGlb, processGltf, writeBinaryGltf } = require('gltf-pipeline');

function parseArgs(argv) {
    const args = {
        in: null, out: null,
        cl: 10,        // compressionLevel 0..10
        qpos: 14,      // quantizePositionBits
        qnor: 10,      // quantizeNormalBits
        quv: 12,       // quantizeTexcoordBits
        qgen: 12,      // quantizeGenericBits
        unified: false // 是否统一量化（减少接缝）
    };
    for (let i = 2; i < argv.length; i++) {
        const a = argv[i];
        const next = argv[i+1];
        const flag = (k) => a === `--${k}`;
        if (flag('in'))    { args.in = next; i++; continue; }
        if (flag('out'))   { args.out = next; i++; continue; }
        if (flag('cl'))    { args.cl = Number(next); i++; continue; }
        if (flag('qpos'))  { args.qpos = Number(next); i++; continue; }
        if (flag('qnor'))  { args.qnor = Number(next); i++; continue; }
        if (flag('quv'))   { args.quv = Number(next); i++; continue; }
        if (flag('qgen'))  { args.qgen = Number(next); i++; continue; }
        if (flag('unified')) { args.unified = true; continue; }
    }
    return args;
}

(async () => {
    const args = parseArgs(process.argv);

    if (!args.in || !args.out) {
        console.error('Usage: node compress-gltf.cjs --in <in.glb|in.gltf> --out <out.glb> [--cl 10 --qpos 14 --qnor 10 --quv 12 --qgen 12 --unified]');
        process.exit(2);
    }
    if (path.resolve(args.in) === path.resolve(args.out)) {
        console.error('Refuse to overwrite input. Please use a different --out path.');
        process.exit(2);
    }
    if (!fs.existsSync(args.in)) {
        console.error(`Input not found: ${args.in}`);
        process.exit(2);
    }

    // gltf-pipeline 选项
    const options = {
        dracoOptions: {
            compressionLevel: args.cl,
            quantizePositionBits: args.qpos,
            quantizeNormalBits:   args.qnor,
            quantizeTexcoordBits: args.quv,
            quantizeGenericBits:  args.qgen,
            unifiedQuantization:  !!args.unified,
        },
        // 其他可能有用的选项（按需开启）
        // preserve: ['_FEATURE_ID_0', '_FEATURE_ID_1'], // 仅保留未被引用对象；对 Draco 映射没有副作用
        // separate: false,      // 默认输出单 GLB
        // keepUnusedElements: true, // 不建议长期开启，可能导致残留垃圾
    };

    const ext = path.extname(args.in).toLowerCase();
    try {
        if (ext === '.glb') {
            const input = fs.readFileSync(args.in);
            const results = await processGlb(input, options);
            fs.writeFileSync(args.out, results.glb);
        } else if (ext === '.gltf') {
            const gltfJson = JSON.parse(fs.readFileSync(args.in, 'utf8'));
            const assetDir = path.dirname(args.in);
            // 让 gltf-pipeline 能找到相对路径的资源
            const results = await processGltf(gltfJson, {
                ...options,
                resourceDirectory: assetDir,
            });
            // 输出为 GLB（也可改写为写 .gltf + 外链资源）
            const outDir = path.dirname(args.out);
            if (!fs.existsSync(outDir)) fs.mkdirSync(outDir, { recursive: true });
            await writeBinaryGltf(results.gltf, args.out);
        } else {
            console.error('Unsupported input extension. Use .glb or .gltf');
            process.exit(2);
        }
    } catch (err) {
        console.error('Compression failed:\n', err?.stack || err);
        process.exit(1);
    }

    // 简单统计
    try {
        const inBytes = fs.statSync(args.in).size;
        const outBytes = fs.statSync(args.out).size;
        const pct = (outBytes / Math.max(1, inBytes) * 100).toFixed(2);
        console.log(`OK: ${path.basename(args.in)} (${(inBytes/1024).toFixed(2)} KB) → ${path.basename(args.out)} (${(outBytes/1024).toFixed(2)} KB), ${pct}%`);
    } catch {}
})();
