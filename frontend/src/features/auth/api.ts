import { request, type TokenResponse } from "@/shared/api/client";

export function login(username: string, password: string): Promise<TokenResponse> {
  return request<TokenResponse>("/auth/login", {
    method: "POST",
    headers: { "Content-Type": "application/json", "X-Auth-Mode": "browser" },
    body: JSON.stringify({ username, password }),
  });
}

export function register(
  username: string,
  password: string,
  roles?: string[],
): Promise<TokenResponse> {
  return request<TokenResponse>("/auth/register", {
    method: "POST",
    headers: { "Content-Type": "application/json", "X-Auth-Mode": "browser" },
    body: JSON.stringify({ username, password, roles }),
  });
}
