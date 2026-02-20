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
  patchManagerLifecycle: (token, managerId, payload) =>
    request({
      method: "PATCH",
      url: `/super/managers/${managerId}/lifecycle`,
      data: payload,
      headers: withBearer(token),
    }),
  batchManagerLifecycle: (token, payload) =>
    request({
      method: "POST",
      url: "/super/managers/batch-lifecycle",
      data: payload,
      headers: withBearer(token),
    }),
  batchRevokeRenewalKeys: (token, payload) =>
    request({
      method: "POST",
      url: "/super/manager-renewal-keys/batch-revoke",
      data: payload,
      headers: withBearer(token),
    }),
  deleteRenewalKey: (token, id) =>
    request({
      method: "DELETE",
      url: `/super/manager-renewal-keys/${id}`,
      headers: withBearer(token),
    }),
  batchDeleteRenewalKeys: (token, data) =>
    request({
      method: "POST",
      url: "/super/manager-renewal-keys/batch-delete",
      data,
      headers: withBearer(token),
    }),
  resetManagerPassword: (token, managerId, payload) =>
    request({
      method: "PATCH",
      url: `/super/managers/${managerId}/password`,
      data: payload,
      headers: withBearer(token),
    }),
  listAuditLogs: (token, params = {}) =>
    request({
      method: "GET",
      url: "/super/audit-logs",
      params,
      headers: withBearer(token),
    }),
};

export const managerApi = {
  register: (payload) =>
    request({ method: "POST", url: "/manager/auth/register", data: payload }),
  login: (payload) =>
    request({ method: "POST", url: "/manager/auth/login", data: payload }),
  me: (token) =>
    request({
      method: "GET",
      url: "/manager/auth/me",
      headers: withBearer(token),
    }),
  redeemRenewalKey: (token, payload) =>
    request({
      method: "POST",
      url: "/manager/auth/redeem-renewal-key",
      data: payload,
      headers: withBearer(token),
    }),
  putMeAlias: (token, payload) =>
    request({
      method: "PUT",
      url: "/manager/me/alias",
      data: payload,
      headers: withBearer(token),
    }),
  overview: (token) =>
    request({
      method: "GET",
      url: "/manager/overview",
      headers: withBearer(token),
    }),
  taskPool: (token, params = {}) =>
    request({
      method: "GET",
      url: "/manager/task-pool",
      params,
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
  getUserLogs: (token, userId, params = {}) =>
    request({
      method: "GET",
      url: `/manager/users/${userId}/logs`,
      params,
      headers: withBearer(token),
    }),
  deleteUserLogs: (token, userId) =>
    request({
      method: "DELETE",
      url: `/manager/users/${userId}/logs`,
      headers: withBearer(token),
    }),
  batchUserLifecycle: (token, payload) =>
    request({
      method: "POST",
      url: "/manager/users/batch-lifecycle",
      data: payload,
      headers: withBearer(token),
    }),
  batchUserAssets: (token, payload) =>
    request({
      method: "POST",
      url: "/manager/users/batch-assets",
      data: payload,
      headers: withBearer(token),
    }),
  deleteUser: (token, userId) =>
    request({
      method: "DELETE",
      url: `/manager/users/${userId}`,
      headers: withBearer(token),
    }),
  batchDeleteUsers: (token, payload) =>
    request({
      method: "POST",
      url: "/manager/users/batch-delete",
      data: payload,
      headers: withBearer(token),
    }),
  batchRevokeActivationCodes: (token, payload) =>
    request({
      method: "POST",
      url: "/manager/activation-codes/batch-revoke",
      data: payload,
      headers: withBearer(token),
    }),
  deleteActivationCode: (token, id) =>
    request({
      method: "DELETE",
      url: `/manager/activation-codes/${id}`,
      headers: withBearer(token),
    }),
  batchDeleteActivationCodes: (token, data) =>
    request({
      method: "POST",
      url: "/manager/activation-codes/batch-delete",
      data,
      headers: withBearer(token),
    }),
  getDuiyiAnswers: (token) =>
    request({
      method: "GET",
      url: "/manager/duiyi-answers",
      headers: withBearer(token),
    }),
  putDuiyiAnswers: (token, payload) =>
    request({
      method: "PUT",
      url: "/manager/duiyi-answers",
      data: payload,
      headers: withBearer(token),
    }),
};

export const userApi = {
  registerByCode: (payload) =>
    request({ method: "POST", url: "/user/auth/register-by-code", data: payload }),
  login: (payload) =>
    request({ method: "POST", url: "/user/auth/login", data: payload }),
  logout: (token) =>
    request({
      method: "POST",
      url: "/user/auth/logout",
      headers: withBearer(token),
    }),
  getMeProfile: (token) =>
    request({
      method: "GET",
      url: "/user/me/profile",
      headers: withBearer(token),
    }),
  putMeProfile: (token, payload) =>
    request({
      method: "PUT",
      url: "/user/me/profile",
      data: payload,
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
  getMeLogs: (token, params = {}) =>
    request({
      method: "GET",
      url: "/user/me/logs",
      params,
      headers: withBearer(token),
    }),
  getMeLineup: (token) =>
    request({
      method: "GET",
      url: "/user/me/lineup",
      headers: withBearer(token),
    }),
  putMeLineup: (token, payload) =>
    request({
      method: "PUT",
      url: "/user/me/lineup",
      data: payload,
      headers: withBearer(token),
    }),
  scanCreate: (token, payload) =>
    request({ method: "POST", url: "/user/scan/create", data: payload, headers: withBearer(token) }),
  scanStatus: (token) =>
    request({ method: "GET", url: "/user/scan/status", headers: withBearer(token) }),
  scanChoice: (token, payload) =>
    request({ method: "POST", url: "/user/scan/choice", data: payload, headers: withBearer(token) }),
  scanCancel: (token, payload) =>
    request({ method: "POST", url: "/user/scan/cancel", data: payload, headers: withBearer(token) }),
  scanHeartbeat: (token, payload) =>
    request({ method: "POST", url: "/user/scan/heartbeat", data: payload, headers: withBearer(token) }),
};
