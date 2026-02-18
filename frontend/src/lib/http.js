import axios from "axios";

const baseURL = import.meta.env.VITE_API_BASE || "/api/v1";
const rootApiBase = import.meta.env.VITE_ROOT_API_BASE || "";

const http = axios.create({
  baseURL,
  timeout: 15000,
});

const rootHttp = axios.create({
  baseURL: rootApiBase,
  timeout: 15000,
});

export function withBearer(token) {
  return token ? { Authorization: `Bearer ${token}` } : {};
}

export async function request(config) {
  const response = await http.request(config);
  return response.data;
}

export async function rootRequest(config) {
  const response = await rootHttp.request(config);
  return response.data;
}

export function parseApiError(error) {
  return (
    error?.response?.data?.detail ||
    error?.response?.data?.message ||
    error?.message ||
    "请求失败"
  );
}

export const commonApi = {
  health: () => rootRequest({ method: "GET", url: "/health" }),
  schedulerStatus: () => request({ method: "GET", url: "/scheduler/status" }),
  taskTemplates: (userType = "") =>
    request({
      method: "GET",
      url: "/task-templates",
      params: userType ? { user_type: userType } : {},
    }),
};

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
  listManagerRenewalKeys: (token, params = {}) =>
    request({
      method: "GET",
      url: "/super/manager-renewal-keys",
      params,
      headers: withBearer(token),
    }),
  listManagers: (token, params = {}) =>
    request({
      method: "GET",
      url: "/super/managers",
      params,
      headers: withBearer(token),
    }),
  patchManagerRenewalKeyStatus: (token, keyId, payload) =>
    request({
      method: "PATCH",
      url: `/super/manager-renewal-keys/${keyId}/status`,
      data: payload,
      headers: withBearer(token),
    }),
  patchManagerStatus: (token, managerId, payload) =>
    request({
      method: "PATCH",
      url: `/super/managers/${managerId}/status`,
      data: payload,
      headers: withBearer(token),
    }),
  patchManagerLifecycle: (token, managerId, payload) =>
    request({
      method: "PATCH",
      url: `/super/managers/${managerId}/lifecycle`,
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
  overview: (token) =>
    request({
      method: "GET",
      url: "/manager/overview",
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
  patchUserLifecycle: (token, userId, payload) =>
    request({
      method: "PATCH",
      url: `/manager/users/${userId}/lifecycle`,
      data: payload,
      headers: withBearer(token),
    }),
  getUserAssets: (token, userId) =>
    request({
      method: "GET",
      url: `/manager/users/${userId}/assets`,
      headers: withBearer(token),
    }),
  putUserAssets: (token, userId, payload) =>
    request({
      method: "PUT",
      url: `/manager/users/${userId}/assets`,
      data: payload,
      headers: withBearer(token),
    }),
  listUsers: (token, params = {}) =>
    request({
      method: "GET",
      url: "/manager/users",
      params,
      headers: withBearer(token),
    }),
  listActivationCodes: (token, params = {}) =>
    request({
      method: "GET",
      url: "/manager/activation-codes",
      params,
      headers: withBearer(token),
    }),
  patchActivationCodeStatus: (token, codeId, payload) =>
    request({
      method: "PATCH",
      url: `/manager/activation-codes/${codeId}/status`,
      data: payload,
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
  logout: (token, payload = { all: false }) =>
    request({
      method: "POST",
      url: "/user/auth/logout",
      data: payload,
      headers: withBearer(token),
    }),
  getMeProfile: (token) =>
    request({
      method: "GET",
      url: "/user/me/profile",
      headers: withBearer(token),
    }),
  getMeAssets: (token) =>
    request({
      method: "GET",
      url: "/user/me/assets",
      headers: withBearer(token),
    }),
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
