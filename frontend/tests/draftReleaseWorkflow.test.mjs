import { test } from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync, mkdtempSync, writeFileSync, mkdirSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import path from 'node:path'
import { spawnSync } from 'node:child_process'
import YAML from 'yaml'

const workflow = YAML.parse(readFileSync(new URL('../../.github/workflows/build-macos.yml', import.meta.url), 'utf8'))
const draft = workflow.jobs['draft-release']
const step = name => draft.steps.find(item => item.name === name)

function selected(expression, event, release) {
  const js = expression.replace(/^\$\{\{\s*|\s*\}\}$/g, '')
  return Function('github', 'inputs', `return Boolean(${js})`)({ event_name: event }, { draft_release: release })
}

test('draft dispatch selects only the direct-release job; other events keep the old route', () => {
  for (const [event, release] of [['workflow_dispatch', true], ['workflow_dispatch', false], ['push', false], ['push', true]]) {
    const expected = event === 'workflow_dispatch' && release
    assert.equal(selected(workflow.jobs.build.if, event, release), !expected)
    assert.equal(selected(draft.if, event, release), expected)
  }
})

test('draft job uses a standard arm64 runner, checks before packaging, and never caches or uploads artifacts', () => {
  assert.equal(draft['runs-on'], 'macos-15')
  assert.equal(draft.env.GOARCH, 'arm64')
  for (const item of draft.steps) {
    assert.doesNotMatch(item.uses || '', /upload-artifact|actions\/cache/)
    assert.doesNotMatch(item.run || '', /gh\s+cache\s+upload|actions\/upload-artifact/)
    if ((item.uses || '').startsWith('actions/setup-go')) assert.equal(item.with.cache, false)
    if ((item.uses || '').startsWith('actions/setup-node')) assert.equal(item.with.cache, undefined)
  }
  assert.ok(draft.steps.indexOf(step('Check frontend before packaging')) < draft.steps.indexOf(step('Build macOS app')))
  assert.match(step('Check frontend before packaging').run, /npm run check && npm run lint/)
  assert.equal(step('Check out merged app source').with.ref, '${{ steps.source.outputs.sha }}')
  const publish = step('Publish draft test release directly').run
  assert.match(publish, /--target "\$BUILD_SHA" --draft --prerelease/)
  assert.match(publish, /shasum -a 256/)
})

test('actual source guard accepts latest main and rejects stale SHA, invalid SHA and non-arm64 requests', () => {
  const temp = mkdtempSync(path.join(tmpdir(), 'draft-route-'))
  try {
    const gh = path.join(temp, 'gh')
    writeFileSync(gh, '#!/bin/sh\nprintf "%s\\n" "$MOCK_MAIN"\n', { mode: 0o700 })
    const sha = 'a'.repeat(40)
    const script = step('Resolve latest merged main').run
    assert.equal(step('Resolve latest merged main').env.EXPECTED_SHA, '${{ inputs.source_sha }}')
    assert.equal(step('Resolve latest merged main').env.REQUESTED_ARCH, '${{ inputs.architecture }}')
    for (const [expected, main, arch, success] of [['', sha, 'arm64', true], [sha, sha, 'arm64', true], ['b'.repeat(40), sha, 'arm64', false], ['', 'bad-sha', 'arm64', false], ['', '', 'arm64', false], ['', 'a'.repeat(39), 'arm64', false], ['', 'a'.repeat(41), 'arm64', false], ['', 'g'.repeat(40), 'arm64', false], ['', `${sha}\n${sha}`, 'arm64', false], ['', sha, 'amd64', false]]) {
      const out = path.join(temp, 'out')
      writeFileSync(out, '')
      const result = spawnSync('bash', ['-c', script], { encoding: 'utf8', env: { ...process.env, PATH: `${temp}:${process.env.PATH}`, EXPECTED_SHA: expected, MOCK_MAIN: main, REQUESTED_ARCH: arch, GH_REPO: 'owner/repo', GITHUB_OUTPUT: out, GITHUB_ENV: out } })
      assert.equal(result.status === 0, success, `${expected}/${main}/${arch}: ${result.stderr}`)
      if (success) assert.match(readFileSync(out, 'utf8'), new RegExp(`BUILD_SHA=${sha}`))
      if (!success) assert.equal(readFileSync(out, 'utf8'), '', 'rejected source must not be exported')
    }
  } finally { rmSync(temp, { recursive: true, force: true }) }
})

test('actual architecture check uses the configured executable and rejects wrong slices', () => {
  const temp = mkdtempSync(path.join(tmpdir(), 'draft-arch-'))
  try {
    const executable = JSON.parse(readFileSync(new URL('../../wails.json', import.meta.url), 'utf8')).outputfilename
    const binaryDir = path.join(temp, 'build/bin/postilaatikko.app/Contents/MacOS')
    mkdirSync(binaryDir, { recursive: true })
    writeFileSync(path.join(binaryDir, executable), '')
    writeFileSync(path.join(binaryDir, 'postilaatikko-helper'), '')
    writeFileSync(path.join(temp, 'lipo'), '#!/bin/sh\ntest -f "$2" || exit 1\nprintf "%s\\n" "$MOCK_ARCH"\n', { mode: 0o700 })
    for (const arch of ['arm64', 'x86_64', 'arm64 x86_64']) {
      const result = spawnSync('bash', ['-e', '-c', step('Verify arm64-only app and helper').run], { cwd: temp, encoding: 'utf8', env: { ...process.env, PATH: `${temp}:${process.env.PATH}`, MOCK_ARCH: arch } })
      assert.equal(result.status === 0, arch === 'arm64', `${arch}: ${result.stderr}`)
    }
  } finally { rmSync(temp, { recursive: true, force: true }) }
})
