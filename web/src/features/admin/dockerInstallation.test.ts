import { spawnSync } from 'node:child_process'

import { describe, expect, it } from 'vitest'

import { agentDockerImage, dockerInstallCommand } from './dockerInstallation'

function parseCommands(command: string): string[][] {
  // Observe real POSIX-shell argv through a harmless sudo function, rather
  // than asserting the output of a second quoting implementation.
  const result = spawnSync('/bin/sh', ['-c', `sudo() { printf '%s\\0' "$@"; printf '\\037'; }; ${command}`], { encoding: 'utf8' })
  expect(result.status).toBe(0)
  expect(result.stderr).toBe('')
  return result.stdout.split('\x1f').filter(Boolean).map((part) => part.split('\0').slice(0, -1))
}

describe('Docker Agent installation command', () => {
  const enrollment = { id: 'ABcd_-012345678901234567890123456789012345678', enrollmentToken: 'one-time-token' }
  const controlUrl = 'https://control.example.com/panel'

  it('enrolls once transiently and starts with the same persistent credential volume', () => {
    const command = dockerInstallCommand(enrollment, controlUrl)
    expect(command).not.toBeNull()
    const [pull, volume, enroll, run] = parseCommands(command ?? '')
    expect(pull).toEqual(['docker', 'pull', agentDockerImage])
    expect(volume).toEqual(['docker', 'volume', 'create', `vps-agent-data-${enrollment.id}`])
    expect(enroll).toContain('--rm')
    expect(enroll.slice(-7)).toEqual(['enroll', '--control-url', controlUrl, '--token', enrollment.enrollmentToken, '--config', '/data/agent.json'])
    expect(run).toContain('--detach')
    expect(run).toContain('unless-stopped')
    expect(run).toContain(`vps-agent-${enrollment.id}`)
    const mount = `type=volume,source=${volume[3]},target=/data`
    expect(enroll).toContain(mount)
    expect(run).toContain(mount)
    expect(enroll).toContain(agentDockerImage)
    expect(run.at(-1)).toBe(agentDockerImage)
    expect(run).not.toContain(enrollment.enrollmentToken)
    expect(run).not.toContain('--token')
    expect(run).not.toContain('--env')
    expect(run).not.toContain('enroll')
    expect(volume[3].length).toBeLessThanOrEqual(63)
  })

  it('uses host namespaces and separate read-only sources without recursive root submounts', () => {
    const [, , enroll, run] = parseCommands(dockerInstallCommand(enrollment, controlUrl) ?? '')
    for (const args of [enroll, run]) {
      expect(args).toContain('--read-only')
      expect(args.slice(args.indexOf('--cap-drop'), args.indexOf('--cap-drop') + 2)).toEqual(['--cap-drop', 'ALL'])
      expect(args).toContain('no-new-privileges')
      expect(args.slice(args.indexOf('--network'), args.indexOf('--network') + 2)).toEqual(['--network', 'host'])
      expect(args).not.toContain('--privileged')
      expect(args).not.toContain('--cap-add')
    }
    for (const namespace of ['--pid', '--uts']) expect(run.slice(run.indexOf(namespace), run.indexOf(namespace) + 2)).toEqual([namespace, 'host'])
    expect(run).toContain('type=bind,source=/proc,target=/host/proc,readonly')
    expect(run).toContain('type=bind,source=/sys,target=/host/sys,readonly')
    expect(run).toContain('type=bind,source=/,target=/host/root,readonly,bind-recursive=disabled')
    expect(run).toContain('type=bind,source=/etc/os-release,target=/host/os-release,readonly')
    expect(run.join(' ')).not.toContain('docker.sock')
  })

  it('passes quotes, spaces, newlines and shell expressions literally through the actual shell', () => {
    const enrollmentToken = `a' b"; $(printf unexpected >&2) \`printf unexpected >&2\`\n!$TOKEN`
    const url = `https://control.example.com/a' b;$(printf unexpected >&2)`
    const [, , enroll, run] = parseCommands(dockerInstallCommand({ ...enrollment, enrollmentToken }, url) ?? '')
    expect(enroll[enroll.indexOf('--token') + 1]).toBe(enrollmentToken)
    expect(enroll[enroll.indexOf('--control-url') + 1]).toBe(url)
    expect(run.join(' ')).not.toContain(enrollmentToken)
  })

  it('does not start the persistent runtime when enrollment fails', () => {
    const command = dockerInstallCommand(enrollment, controlUrl)
    const result = spawnSync('/bin/sh', ['-c', `sudo() { case " $* " in *" enroll "*) return 17;; *" --detach "*) printf unexpected;; esac; }; ${command}`], { encoding: 'utf8' })
    expect(result.status).toBe(17)
    expect(result.stdout).toBe('')
  })

  it('uses distinct complete IDs and rejects IDs that cannot be safely named', () => {
    expect(dockerInstallCommand({ ...enrollment, id: 'abc-def' }, controlUrl)).not.toBe(dockerInstallCommand({ ...enrollment, id: 'abc_def' }, controlUrl))
    for (const id of ['', 'a'.repeat(49), 'a b', 'a/b', 'a;exit', "a'", 'a\n', 'a$(id)']) {
      expect(dockerInstallCommand({ ...enrollment, id }, controlUrl)).toBeNull()
    }
    expect(dockerInstallCommand({ ...enrollment, enrollmentToken: 'a\0b' }, controlUrl)).toBeNull()
    expect(dockerInstallCommand(enrollment, 'https://example.com/\0')).toBeNull()
  })
})
