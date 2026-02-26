export function statusTagType(status) {
  if (status === "active") return "success";
  if (status === "disabled") return "danger";
  if (status === "expired") return "warning";
  return "info";
}

export function keyStatusTagType(status) {
  if (status === "unused") return "success";
  if (status === "used") return "warning";
  if (status === "revoked") return "danger";
  if (status === "deleted") return "danger";
  return "info";
}

export function userTypeLabel(userType) {
  if (userType === "duiyi") return "对弈竞猜";
  if (userType === "shuaka") return "刷卡";
  if (userType === "foster") return "寄养";
  if (userType === "jingzhi") return "精致日常";
  return "日常";
}

export function patchSummary(target, source, keys) {
  keys.forEach((key) => {
    const value = Number(source?.[key] ?? 0);
    target[key] = Number.isFinite(value) ? value : 0;
  });
}

export function ensureTaskConfig(taskCfg) {
  const merged = {
    enabled: false,
    next_time: "",
    fail_delay: 30,
    ...(taskCfg || {}),
  };
  if (typeof merged.enabled !== "boolean") merged.enabled = false;
  if (typeof merged.next_time !== "string") merged.next_time = "";
  if (typeof merged.fail_delay !== "number") merged.fail_delay = Number(merged.fail_delay || 0);
  return merged;
}

export function parseTaskConfigFromRaw(rawString) {
  try {
    const parsed = JSON.parse(rawString || "{}");
    return parsed && typeof parsed === "object" ? parsed : {};
  } catch {
    return {};
  }
}

export const ASSET_FIELDS = [
  { key: "stamina", label: "体力" },
  { key: "gouyu", label: "勾玉" },
  { key: "lanpiao", label: "蓝票" },
  { key: "gold", label: "金币" },
  { key: "gongxun", label: "功勋" },
  { key: "xunzhang", label: "勋章" },
  { key: "tupo_ticket", label: "突破票" },
  { key: "fanhe_level", label: "饭盒等级" },
  { key: "jiuhu_level", label: "酒壶等级" },
  { key: "liao_level", label: "寮等级" },
];

export const USER_TYPE_OPTIONS = [
  { value: "daily", label: "日常" },
  { value: "duiyi", label: "对弈竞猜" },
  { value: "shuaka", label: "刷卡" },
  { value: "foster", label: "寄养" },
  { value: "jingzhi", label: "精致日常" },
];

export const MANAGER_TYPE_OPTIONS = [
  { value: "daily", label: "日常" },
  { value: "shuaka", label: "刷卡" },
  { value: "duiyi", label: "对弈竞猜" },
  { value: "all", label: "全部" },
];

export function managerTypeLabel(managerType) {
  const map = { daily: "日常", shuaka: "刷卡", duiyi: "对弈竞猜", all: "全部" };
  return map[managerType] || managerType || "-";
}

export function formatTime(isoString) {
  if (!isoString || isoString === "-") return "-";
  const d = new Date(isoString);
  if (Number.isNaN(d.getTime())) return isoString;
  const pad = (n) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

export function statusLabel(status) {
  const map = { active: "未过期", expired: "已过期", disabled: "已禁用" };
  return map[status] || status || "-";
}

export function keyStatusLabel(status) {
  const map = { unused: "未使用", used: "已使用", revoked: "已撤销", deleted: "已删除" };
  return map[status] || status || "-";
}

export function jobStatusTagType(status) {
  const map = { pending: "info", leased: "info", running: "", success: "success", failed: "danger" };
  return map[status] || "info";
}

export function jobStatusLabel(status) {
  const map = { pending: "等待中", leased: "等待中", running: "执行中", success: "执行成功", failed: "执行失败" };
  return map[status] || status || "-";
}

export function eventTypeLabel(eventType) {
  const map = {
    leased: "已获取",
    start: "开始执行",
    success: "执行成功",
    fail: "执行失败",
  };
  return map[eventType] || eventType || "-";
}

export function eventTypeTagType(eventType) {
  const map = {
    success: "success",
    fail: "danger",
    start: "",
    leased: "info",
  };
  return map[eventType] || "info";
}

export function errorCodeLabel(code) {
  if (!code) return "";
  const map = {
    LOCAL_ACCOUNT_NOT_MAPPED: "本地账号未映射",
    LOCAL_BATCH_FAILED: "本地执行失败",
    LOCAL_ACCOUNT_MISSING: "缺少本地账号",
    TASK_TYPE_INVALID: "任务类型无效",
  };
  return map[code] || code;
}

export function isHHmmPattern(value) {
  return /^\d{2}:\d{2}$/.test(value);
}

export const SPECIAL_TASK_NAMES = new Set([
  '探索突破', '结界卡合成', '寮商店', '每周商店', '御魂', '斗技', '对弈竞猜', '寄养', '放卡',
]);

// 探索突破可配置的中断任务选项
export const INTERRUPTABLE_TASK_OPTIONS = [
  "寄养", "悬赏", "弥助", "勾协", "领取登录礼包", "领取邮件",
  "爬塔", "逢魔", "地鬼", "道馆", "寮商店", "领取寮金币",
  "每日一抽", "每周商店", "秘闻", "签到", "御魂", "每周分享",
  "召唤礼包", "领取饭盒酒壶", "斗技", "对弈竞猜", "加好友",
  "领取成就奖励",
  "放卡",
];

// 寄养奖励优先级选项
export const FOSTER_REWARD_OPTIONS = [
  { value: "6xtg", label: "6星太鼓" },
  { value: "6xdy", label: "6星大引" },
  { value: "5xtg", label: "5星太鼓" },
  { value: "5xdy", label: "5星大引" },
  { value: "4xtg", label: "4星太鼓" },
  { value: "4xdy", label: "4星大引" },
];

export function auditActionLabel(action) {
  const map = {
    create_manager_renewal_key: "创建续费密钥",
    patch_manager_renewal_key_status: "撤销续费密钥",
    batch_revoke_renewal_keys: "批量撤销密钥",
    delete_manager_renewal_key: "删除续费密钥",
    batch_delete_renewal_keys: "批量删除密钥",
    patch_manager_lifecycle: "修改管理员有效期",
    reset_manager_password: "重置管理员密码",
    batch_manager_lifecycle: "批量修改管理员有效期",
    redeem_manager_renewal_key: "使用续费密钥",
    create_blogger: "创建博主",
    delete_blogger: "删除博主",
    set_blogger_answer: "配置博主答案",
    set_duiyi_answer: "配置对弈答案",
  };
  return map[action] || action || "-";
}

export function actorTypeLabel(actorType) {
  const map = { super: "超级管理员", manager: "管理员" };
  return map[actorType] || actorType || "-";
}

export function auditActionTagType(action) {
  if (!action) return "info";
  if (action.startsWith("create_") || action === "redeem_manager_renewal_key") return "success";
  if (action.startsWith("delete_") || action.startsWith("batch_delete_")) return "danger";
  if (action.includes("revoke")) return "warning";
  if (action.startsWith("reset_")) return "warning";
  return "info";
}

export const AUDIT_ACTION_OPTIONS = [
  { value: "create_manager_renewal_key", label: "创建续费密钥" },
  { value: "delete_manager_renewal_key", label: "删除续费密钥" },
  { value: "batch_delete_renewal_keys", label: "批量删除密钥" },
  { value: "patch_manager_renewal_key_status", label: "撤销续费密钥" },
  { value: "batch_revoke_renewal_keys", label: "批量撤销密钥" },
  { value: "redeem_manager_renewal_key", label: "使用续费密钥" },
  { value: "patch_manager_lifecycle", label: "修改管理员有效期" },
  { value: "batch_manager_lifecycle", label: "批量修改管理员有效期" },
  { value: "reset_manager_password", label: "重置管理员密码" },
  { value: "create_blogger", label: "创建博主" },
  { value: "delete_blogger", label: "删除博主" },
  { value: "set_blogger_answer", label: "配置博主答案" },
  { value: "set_duiyi_answer", label: "配置对弈答案" },
];

export async function copyToClipboard(text) {
  if (navigator.clipboard && window.isSecureContext) {
    await navigator.clipboard.writeText(text);
    return;
  }
  // 降级：HTTP 环境使用 execCommand
  const ta = document.createElement('textarea');
  ta.value = text;
  ta.style.cssText = 'position:fixed;top:0;left:0;width:1px;height:1px;opacity:0;pointer-events:none;';
  document.body.appendChild(ta);
  ta.focus();
  ta.select();
  try {
    if (!document.execCommand('copy')) throw new Error('execCommand failed');
  } finally {
    document.body.removeChild(ta);
  }
}
