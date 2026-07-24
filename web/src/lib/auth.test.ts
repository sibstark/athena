import { describe, expect, it, vi } from "vitest"

import { requestLogin, requestLogout, requestSession } from "./auth"

describe("auth API", () => {
  it("проверяет сессию с cookie и преобразует 401 в unauthenticated", async () => {
    const fetchImpl = vi
      .fn()
      .mockResolvedValue(new Response(null, { status: 401 }))
    await expect(requestSession(fetchImpl)).resolves.toEqual({
      authenticated: false,
    })
    expect(fetchImpl).toHaveBeenCalledWith(
      "/v1/auth/session",
      expect.objectContaining({ method: "GET", credentials: "include" }),
    )
  })

  it("отправляет credentials при login и logout", async () => {
    const fetchImpl = vi
      .fn()
      .mockResolvedValueOnce(
        new Response(
          JSON.stringify({ authenticated: true, username: "admin" }),
          { status: 200 },
        ),
      )
      .mockResolvedValueOnce(
        new Response(JSON.stringify({ authenticated: false }), { status: 200 }),
      )

    await expect(requestLogin("admin", "password", fetchImpl)).resolves.toEqual(
      {
        authenticated: true,
        username: "admin",
      },
    )
    await expect(requestLogout(fetchImpl)).resolves.toBeUndefined()
    expect(fetchImpl).toHaveBeenNthCalledWith(
      1,
      "/v1/auth/login",
      expect.objectContaining({ credentials: "include" }),
    )
    expect(fetchImpl).toHaveBeenNthCalledWith(
      2,
      "/v1/auth/logout",
      expect.objectContaining({ credentials: "include" }),
    )
  })
})
