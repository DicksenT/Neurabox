#!/usr/bin/env node

const fs = require('fs');
const path = require('path');
const os = require('os');
const https = require('https');
const { exec } = require('child_process');

const VERSION = 'v0.2.0';
const REPO = 'DicksenT/neurabox';

function getBinaryInfo() {
    const platform = os.platform();
    const arch = os.arch();

    const map = {
        win32: { 
            name: arch === 'arm64' ? 'neurabox-windows-arm64.exe' : 'neurabox-windows.exe',
            ext: '.exe'
        },
        linux: { 
            name: arch === 'arm64' ? 'neurabox-linux-arm64' : 'neurabox-linux',
            ext: ''
        },
        darwin: { 
            name: arch === 'arm64' ? 'neurabox-macos-arm64' : 'neurabox-macos',
            ext: ''
        },
    };

    if (!map[platform]) {
        console.error(` Unsupported platform: ${platform}/${arch}`);
        process.exit(1);
    }

    return map[platform];
}

function downloadBinary(url, dest) {
    return new Promise((resolve, reject) => {
        const file = fs.createWriteStream(dest);
        https.get(url, (response) => {
            if (response.statusCode !== 200) {
                reject(new Error(`HTTP ${response.statusCode}: ${response.statusMessage}`));
                return;
            }
            response.pipe(file);
            file.on('finish', () => {
                file.close();
                resolve();
            });
        }).on('error', reject);
    });
}

async function install() {
    const binInfo = getBinaryInfo();
    const binDir = path.join(__dirname, 'bin');
    const binPath = path.join(binDir, binInfo.name);

    // Create bin directory
    if (!fs.existsSync(binDir)) {
        fs.mkdirSync(binDir, { recursive: true });
    }

    // Download binary from GitHub Releases
    const url = `https://github.com/${REPO}/releases/download/${VERSION}/${binInfo.name}`;
    console.log(` Downloading NeuraBox ${VERSION} for ${os.platform()}/${os.arch()}...`);

    try {
        await downloadBinary(url, binPath);
    } catch (err) {
        console.error(` Failed to download binary: ${err.message}`);
        console.error('   Please download manually from:');
        console.error(`   https://github.com/${REPO}/releases`);
        process.exit(1);
    }

    // Make executable (Unix)
    if (os.platform() !== 'win32') {
        fs.chmodSync(binPath, 0o755);
    }

    // Create symlink
    const binTarget = path.join(binDir, 'neurabox');
    if (os.platform() === 'win32') {
        fs.copyFileSync(binPath, binTarget + '.exe');
    } else {
        if (fs.existsSync(binTarget)) fs.unlinkSync(binTarget);
        fs.symlinkSync(binInfo.name, binTarget);
    }

    console.log(` NeuraBox installed successfully`);
    console.log(`   Run: neurabox --init`);
}

install().catch(console.error);