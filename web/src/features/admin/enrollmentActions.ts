import { APIError, apiMutation, apiRequest } from '../../api/client'
import { decodeEnrollmentStatus, type EnrollmentStatus } from '../../api/contracts'

export type RevocationResult = { status: 'revoked' } | { status: 'consumed'; enrollment: EnrollmentStatus }

// Both revoke and regenerate use this boundary: a concurrent Agent registration
// refreshes its existing node state instead of creating a second enrollment.
export async function revokeEnrollment(id: string, signal: AbortSignal): Promise<RevocationResult> {
  const path = `/api/v1/admin/enrollments/${encodeURIComponent(id)}`
  try {
    await apiMutation(path, { method: 'DELETE', signal })
    return { status: 'revoked' }
  } catch (error) {
    if (signal.aborted) throw error
    if (error instanceof APIError && error.code === 'enrollment_consumed') {
      return { status: 'consumed', enrollment: await apiRequest(path, decodeEnrollmentStatus, { signal }) }
    }
    throw error
  }
}
