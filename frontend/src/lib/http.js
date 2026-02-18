import axios from "axios";

const baseURL = import.meta.env.VITE_API_BASE || "/api/v1";

const http = axios.create({
  baseURL,
  timeout: 15000,
});

export function withBearer(token) {
  return token ? { Authorization: `Bearer ${token}` } : {};
}

export async function request(config) {
  const response = await http.request(config);
  return response.data;
}

export const superApi = {
  bootstrapStatus: () => request({ method: "GET", url: "/bootstrap/status" }),
  bootstrapInit: (payload) =>
    request({ method: "POST", url: "/bootstrap/init", data: payload }),
  login: (payload) =>
    request({ method: "POST", url: "/super/auth/login", data: payload }),
  createManagerRenewalKey: (token, payload) =>
    request({
      method: "POST",
      url: "/super/manager-renewal-keys",
      data: payload,
      headers: withBearer(token),
    }),
  listManagers: (token) =>
    request({
      method: "GET",
      url: "/super/managers",
      headers: withBearer(token),
    }),
  patchManagerStatus: (token, managerId, payload) =>
    request({
      method: "PATCH",
      url: `/super/managers/${managerId}/status`,
      data: payload,
      headers: withBearer(token),
    }),
};

export const managerApi = {
  register: (payload) =>
    request({ method: "POST", url: "/manager/auth/register", data: payload }),
  login: (payload) =>
    request({ method: "POST", url: "/manager/auth/login", data: payload }),
  redeemRenewalKey: (token, payload) =>
    request({
      method: "POST",
      url: "/manager/auth/redeem-renewal-key",
      data: payload,
      headers: withBearer(token),
    }),
  createActivationCode: (token, payload) =>
    request({
      method: "POST",
      url: "/manager/activation-codes",
      data: payload,
      headers: withBearer(token),
    }),
  quickCreateUser: (token, payload) =>
    request({
      method: "POST",
      url: "/manager/users/quick-create",
      data: payload,
      headers: withBearer(token),
    }),
  listUsers: (token) =>
    request({
      method: "GET",
      url: "/manager/users",
      headers: withBearer(token),
    }),
  getUserTasks: (token, userId) =>
    request({
      method: "GET",
      url: `/manager/users/${userId}/tasks`,
      headers: withBearer(token),
    }),
  putUserTasks: (token, userId, payload) =>
    request({
      method: "PUT",
      url: `/manager/users/${userId}/tasks`,
      data: payload,
      headers: withBearer(token),
    }),
  getUserLogs: (token, userId, limit = 50) =>
    request({
      method: "GET",
      url: `/manager/users/${userId}/logs`,
      params: { limit },
      headers: withBearer(token),
    }),
};

export const userApi = {
  registerByCode: (payload) =>
    request({ method: "POST", url: "/user/auth/register-by-code", data: payload }),
  login: (payload) =>
    request({ method: "POST", url: "/user/auth/login", data: payload }),
  redeemCode: (token, payload) =>
    request({
      method: "POST",
      url: "/user/auth/redeem-code",
      data: payload,
      headers: withBearer(token),
    }),
  getMeTasks: (token) =>
    request({
      method: "GET",
      url: "/user/me/tasks",
      headers: withBearer(token),
    }),
  putMeTasks: (token, payload) =>
    request({
      method: "PUT",
      url: "/user/me/tasks",
      data: payload,
      headers: withBearer(token),
    }),
  getMeLogs: (token, limit = 50) =>
    request({
      method: "GET",
      url: "/user/me/logs",
      params: { limit },
      headers: withBearer(token),
    }),
};
