import type { PasskeyCredential, PasskeyOptions } from './api'

// Passkeys: the phone or computer keeps a key pair and unlocks it with a
// fingerprint, a face or its screen lock. Binary values travel as base64url.

const toBuffer = (s: string): ArrayBuffer => {
  const pad = '='.repeat((4 - (s.length % 4)) % 4)
  const bin = atob((s + pad).replace(/-/g, '+').replace(/_/g, '/'))
  const out = new Uint8Array(bin.length)
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i)
  return out.buffer
}

const toText = (b: ArrayBuffer): string => {
  let bin = ''
  for (const c of new Uint8Array(b)) bin += String.fromCharCode(c)
  return btoa(bin).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '')
}

// Needs a secure context (https, or localhost) and a browser with WebAuthn.
export const passkeysSupported = () => window.isSecureContext && typeof PublicKeyCredential !== 'undefined' && !!navigator.credentials

// Is there something on this device that can verify the user (Face ID, a fingerprint, Windows Hello, a screen lock)?
export async function biometricAvailable(): Promise<boolean> {
  if (!passkeysSupported()) return false
  try {
    return await PublicKeyCredential.isUserVerifyingPlatformAuthenticatorAvailable()
  } catch {
    return false
  }
}

const descriptors = (list?: { type: 'public-key'; id: string }[]): PublicKeyCredentialDescriptor[] | undefined =>
  list?.map((c) => ({ type: c.type, id: toBuffer(c.id), transports: ['internal'] as AuthenticatorTransport[] }))

// Makes a new passkey on this device (asks for the biometric).
export async function createPasskey(o: PasskeyOptions): Promise<PasskeyCredential> {
  const cred = (await navigator.credentials.create({
    publicKey: {
      challenge: toBuffer(o.challenge),
      rp: o.rp!,
      user: { id: toBuffer(o.user!.id), name: o.user!.name, displayName: o.user!.displayName },
      pubKeyCredParams: o.pubKeyCredParams!,
      excludeCredentials: descriptors(o.excludeCredentials),
      authenticatorSelection: o.authenticatorSelection as AuthenticatorSelectionCriteria,
      attestation: 'none',
      timeout: o.timeout,
    },
  })) as PublicKeyCredential | null
  if (!cred) throw new Error('No passkey was made.')
  const r = cred.response as AuthenticatorAttestationResponse
  return { id: cred.id, response: { clientDataJSON: toText(r.clientDataJSON), attestationObject: toText(r.attestationObject) } }
}

// Proves the user is there with a passkey of this device (asks for the biometric).
export async function usePasskey(o: PasskeyOptions): Promise<PasskeyCredential> {
  const cred = (await navigator.credentials.get({
    publicKey: {
      challenge: toBuffer(o.challenge),
      allowCredentials: descriptors(o.allowCredentials),
      userVerification: 'required',
      timeout: o.timeout,
    },
  })) as PublicKeyCredential | null
  if (!cred) throw new Error('The biometric was not confirmed.')
  const r = cred.response as AuthenticatorAssertionResponse
  return { id: cred.id, response: { clientDataJSON: toText(r.clientDataJSON), authenticatorData: toText(r.authenticatorData), signature: toText(r.signature) } }
}

// What went wrong, in words for the user.
export function passkeyError(e: unknown): string {
  const err = e as { name?: string; message?: string }
  switch (err?.name) {
    case 'NotAllowedError':
      return 'Cancelled, or it took too long.'
    case 'InvalidStateError':
      return 'This device already has a passkey.'
    case 'NotSupportedError':
      return 'This device cannot make a passkey Homie supports.'
    case 'SecurityError':
      return 'Passkeys need Homie to be opened over https, at its usual address.'
  }
  return err?.message ?? 'Something went wrong.'
}
