import type { Enrollment } from '../../api/contracts'

export const agentDockerImage = 'ghcr.io/senhao-xu/vps-status-agent:latest'

function shellQuote(value: string): string {
  return `'${value.replaceAll("'", "'\\''")}'`
}

// The complete enrollment ID gives each installation its own credential volume.
// Reject unexpected IDs rather than silently turning two IDs into the same name.
export function dockerInstallCommand(enrollment: Pick<Enrollment, 'id' | 'enrollmentToken'>, controlUrl: string): string | null {
  if (!/^[A-Za-z0-9_-]{1,48}$/.test(enrollment.id) || !enrollment.enrollmentToken || !controlUrl || enrollment.enrollmentToken.includes('\0') || controlUrl.includes('\0')) return null
  const container = `vps-agent-${enrollment.id}`
  const volume = `vps-agent-data-${enrollment.id}`
  const dataMount = `type=volume,source=${volume},target=/data`
  const hardened = ['--read-only', '--cap-drop', 'ALL', '--security-opt', 'no-new-privileges']
  const commands = [
    ['pull', agentDockerImage],
    ['volume', 'create', volume],
    ['run', '--rm', '--network', 'host', ...hardened, '--mount', dataMount, agentDockerImage,
      'enroll', '--control-url', controlUrl, '--token', enrollment.enrollmentToken, '--config', '/data/agent.json'],
    ['run', '--detach', '--name', container, '--restart', 'unless-stopped', '--network', 'host', '--pid', 'host', '--uts', 'host',
      ...hardened, '--mount', dataMount,
      '--mount', 'type=bind,source=/proc,target=/host/proc,readonly',
      '--mount', 'type=bind,source=/sys,target=/host/sys,readonly',
      '--mount', 'type=bind,source=/,target=/host/root,readonly,bind-recursive=disabled',
      '--mount', 'type=bind,source=/etc/os-release,target=/host/os-release,readonly',
      agentDockerImage],
  ]
  return commands.map((args) => `sudo docker ${args.map(shellQuote).join(' ')}`).join(' && \\\n')
}
