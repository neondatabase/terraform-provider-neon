#!/usr/bin/env bash

esbuild index.ts \
  --bundle \
  --platform=node \
  --target=node24 \
  --format=esm \
  --banner:js="import{createRequire as ___cr}from'module';import{fileURLToPath as ___f}from'url';import{dirname as ___d}from'path';const require=___cr(import.meta.url);const __filename=___f(import.meta.url);const __dirname=___d(__filename);" \
  --outfile=index.mjs

zip -j function.zip index.mjs && rm index.mjs
