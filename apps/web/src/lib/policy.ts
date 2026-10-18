/**
 * Client-side policy constants.
 *
 * These mirror the server: the API is the authority (see validateOperatorInput
 * in apps/api/internal/gateway/admin_auth.go), and anything here exists only to
 * give the operator feedback before the request is sent. Keep them in step.
 */

/** Minimum operator password length. Mirrors the API's 8-character floor. */
export const MIN_PASSWORD_LENGTH = 8
