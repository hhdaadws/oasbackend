export const STORAGE_KEYS = {
  superToken: "oas_cloud_super_token",
  managerToken: "oas_cloud_manager_token",
  userToken: "oas_cloud_user_token",
  userAccountNo: "oas_cloud_user_account_no",
};

export function getSession() {
  return {
    superToken: localStorage.getItem(STORAGE_KEYS.superToken) || "",
    managerToken: localStorage.getItem(STORAGE_KEYS.managerToken) || "",
    userToken: localStorage.getItem(STORAGE_KEYS.userToken) || "",
    userAccountNo: localStorage.getItem(STORAGE_KEYS.userAccountNo) || "",
  };
}

export function setSuperToken(token) {
  localStorage.setItem(STORAGE_KEYS.superToken, token || "");
}

export function clearSuperToken() {
  localStorage.removeItem(STORAGE_KEYS.superToken);
}

export function setManagerToken(token) {
  localStorage.setItem(STORAGE_KEYS.managerToken, token || "");
}

export function clearManagerToken() {
  localStorage.removeItem(STORAGE_KEYS.managerToken);
}

export function setUserSession(token, accountNo) {
  localStorage.setItem(STORAGE_KEYS.userToken, token || "");
  localStorage.setItem(STORAGE_KEYS.userAccountNo, accountNo || "");
}

export function clearUserToken() {
  localStorage.removeItem(STORAGE_KEYS.userToken);
}

export function clearUserSession({ keepAccountNo = true } = {}) {
  localStorage.removeItem(STORAGE_KEYS.userToken);
  if (!keepAccountNo) {
    localStorage.removeItem(STORAGE_KEYS.userAccountNo);
  }
}

const SAVED_ACCOUNTS_KEY = "oas_cloud_saved_accounts";

export function getSavedAccounts() {
  try {
    return JSON.parse(localStorage.getItem(SAVED_ACCOUNTS_KEY) || "[]");
  } catch {
    return [];
  }
}

export function upsertSavedAccount({ account_no, login_id, username, status, user_type, archive_status }) {
  const list = getSavedAccounts();
  const idx = list.findIndex((a) => a.account_no === account_no);
  const entry = {
    account_no,
    login_id: login_id || "",
    username: username || "",
    status: status || "active",
    user_type: user_type || "",
    archive_status: archive_status || "normal",
  };
  if (idx >= 0) {
    list[idx] = { ...list[idx], ...entry };
  } else {
    list.unshift(entry);
  }
  localStorage.setItem(SAVED_ACCOUNTS_KEY, JSON.stringify(list));
}

export function removeSavedAccount(accountNo) {
  const list = getSavedAccounts().filter((a) => a.account_no !== accountNo);
  localStorage.setItem(SAVED_ACCOUNTS_KEY, JSON.stringify(list));
}
