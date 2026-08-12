import type { ApiErrorBody } from './types'

export class ApiError extends Error {
  constructor(public status: number, public body: ApiErrorBody) { super(body.message) }
}

export async function api<T>(path: string, init: RequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers)
  if (init.body && !(init.body instanceof FormData)) headers.set('Content-Type', 'application/json')
  const response = await fetch(path, { ...init, headers, credentials: 'same-origin' })
  const body = await response.json().catch(() => ({}))
  if (!response.ok) throw new ApiError(response.status, body as ApiErrorBody)
  return body as T
}

export const json = (value: unknown) => JSON.stringify(value)
