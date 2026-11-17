export type CvarCategory = '基础与连接' | '游戏与对抗' | '插件扩展';
export type CvarValueTone = 'neutral' | 'good' | 'warning' | 'danger';

export interface CvarValueInfo {
  value: string;
  label: string;
  effect: string;
  tone?: CvarValueTone;
}

export interface CvarRangeInfo {
  min: number;
  max: number;
  unit?: string;
  recommended?: string;
  /** true 表示 min/max 来自服务器 help 文本实测的硬性边界，超出会被引擎截断或拒绝。 */
  hardLimit?: boolean;
  lowerEffect: string;
  higherEffect: string;
}

export interface L4D2CvarDefinition {
  name: string;
  displayName: string;
  category: CvarCategory;
  type: '开关' | '数值' | '枚举' | '文本';
  description: string;
  defaultValue: string;
  exampleCommand: string;
  source: string;
  verifiedOnServer?: boolean;
  values?: CvarValueInfo[];
  range?: CvarRangeInfo;
  warning?: string;
}

const booleanValues = (onEffect: string, offEffect: string): CvarValueInfo[] => [
  { value: '0', label: '关闭', effect: offEffect, tone: 'neutral' },
  { value: '1', label: '开启', effect: onEffect, tone: 'good' },
];

// 说明来源统一标注为「服务器内置 help 文本实测」，这是 Source 引擎最权威的
// 变量说明来源，避免凭印象编写描述造成误导。
const serverCvarSource = '插件 ConVar help 文本实测（2026-11-17）';
const engineCvarSource = 'L4D2 / Source 引擎 ConVar，服务器 help 文本实测（2026-11-17）';

export const l4d2CvarCatalog: L4D2CvarDefinition[] = [
  {
    name: 'sv_password',
    displayName: '服务器进入密码',
    category: '基础与连接',
    type: '文本',
    description: '进入多人游戏的服务器密码（引擎原文：Server password for entry into multiplayer games）。留空表示不设密码。',
    defaultValue: '空',
    exampleCommand: 'sm_cvar sv_password ""',
    source: engineCvarSource,
    verifiedOnServer: true,
    values: [
      { value: '""', label: '无密码', effect: '任何玩家都可以直接进入服务器。', tone: 'neutral' },
      { value: '"abc123"', label: '设置密码', effect: '玩家必须输入 abc123 才能进入。', tone: 'good' },
    ],
    warning: '该变量带 notify 标记，改动后立即对新的连接生效；已经进入服务器的玩家不会被踢出。',
  },
  {
    name: 'sv_lan',
    displayName: '局域网模式',
    category: '基础与连接',
    type: '开关',
    description:
      '把服务器标记为局域网服务器（引擎原文：no heartbeat, no authentication, no non-class C addresses）。开启后服务器不再向 Steam 主服务器发送心跳、不参与身份校验，也只接受内网网段连接，通常不会出现在公网服务器列表中。',
    defaultValue: '0',
    exampleCommand: 'sm_cvar sv_lan 0',
    source: engineCvarSource,
    verifiedOnServer: true,
    values: booleanValues(
      '按局域网服务器处理：不向 Steam 发心跳、不做身份校验、拒绝公网网段。',
      '按公网服务器处理，允许公网玩家连接和服务器列表发现。',
    ),
    warning: '公网服误开局域网模式后，玩家可能无法通过公网列表找到服务器，甚至无法从公网连接。',
  },
  {
    name: 'sv_region',
    displayName: '服务器列表地区',
    category: '基础与连接',
    type: '枚举',
    description:
      '向 Steam 服务器浏览器声明这台服务器所属的地区（引擎原文：The region of the world to report this server in）。它只影响列表里的地区筛选与排序，不限制实际可连接的区域。',
    defaultValue: '-1（引擎默认，未显式指定地区；当前服务器为 255）',
    exampleCommand: 'sm_cvar sv_region 4',
    source: engineCvarSource,
    verifiedOnServer: true,
    values: [
      { value: '255', label: '全球', effect: '不归属任何单一地区，最不容易被地区筛选漏掉。', tone: 'good' },
      { value: '4', label: '亚洲', effect: '适合中国大陆、港澳台及其他亚洲地区服务器。', tone: 'good' },
      { value: '3', label: '欧洲', effect: '服务器会被标记为欧洲地区。' },
      { value: '2', label: '南美', effect: '服务器会被标记为南美地区。' },
      { value: '5', label: '澳洲', effect: '服务器会被标记为澳洲地区。' },
      { value: '6', label: '中东', effect: '服务器会被标记为中东地区。' },
      { value: '7', label: '非洲', effect: '服务器会被标记为非洲地区。' },
      { value: '0', label: '美东', effect: '服务器会被标记为美国东部地区。' },
      { value: '1', label: '美西', effect: '服务器会被标记为美国西部地区。' },
    ],
  },
  {
    name: 'sv_consistency',
    displayName: '客户端文件一致性检查',
    category: '基础与连接',
    type: '开关',
    description:
      '是否对关键文件强制客户端与服务器一致（引擎原文：Whether the server enforces file consistency for critical files）。开启后关键资源不一致的客户端会被拒绝，使用自定义模型、音效等模组时通常需要关闭。',
    defaultValue: '1',
    exampleCommand: 'sm_cvar sv_consistency 0',
    source: engineCvarSource,
    verifiedOnServer: true,
    values: booleanValues(
      '要求客户端文件一致，可减少篡改，但自制内容更容易导致进服失败。',
      '关闭一致性检查，允许客户端使用不同版本的模型、音效和部分资源。',
    ),
  },
  {
    name: 'sv_steam_bypass',
    displayName: '绕过 Steam 身份校验',
    category: '基础与连接',
    type: '开关',
    description:
      '服务器内置说明为「Bypass steam validation」，即跳过对客户端 SteamID 的官方校验。开启后服务器不再向 Steam 核对玩家身份，可缓解部分玩家出现的 No Steam logon（错误码 6）掉线问题。',
    defaultValue: '0',
    exampleCommand: 'sm_cvar sv_steam_bypass 1',
    source: engineCvarSource,
    verifiedOnServer: true,
    values: booleanValues(
      '跳过 SteamID 校验，只对开启后新进入的玩家生效。',
      '按原版流程向 Steam 校验每个玩家的 SteamID。',
    ),
    warning:
      '这不是「大厅」或「人数」相关参数。开启后 SteamID 不再被官方验证，改名客户端可能冒充他人 SteamID，也会让家庭共享等限制失效；只在对掉线问题无法回避时临时使用。',
  },
  {
    name: 'sv_force_unreserved',
    displayName: '禁止大厅预留',
    category: '基础与连接',
    type: '开关',
    description:
      '服务器内置说明为「Disallow lobby reservation」，即禁止服务器接受 Steam 大厅的预留请求。开启后服务器不再回应大厅的预留询问，并拒绝携带预留凭据的加入，因此大厅匹配不到这台服务器，但空位不会被大厅占用。',
    defaultValue: '0',
    exampleCommand: 'sm_cvar sv_force_unreserved 1',
    source: engineCvarSource,
    verifiedOnServer: true,
    values: booleanValues(
      '禁止大厅预留：服务器不再回应大厅预留请求，只能直连进服。',
      '允许大厅预留：服务器正常参与大厅匹配，可能被预留名额。',
    ),
    warning:
      '多人数（超过 4 人）服通常开启，否则大厅会在满 4 人后把剩余名额预留掉；代价是大厅匹配流量基本消失，只能靠直连或分享 IP 进服。',
  },
  {
    name: 'sv_cookie',
    displayName: '大厅预留凭据',
    category: '基础与连接',
    type: '数值',
    description:
      '服务器内置说明为「Lobby reservation cookie」，保存当前生效的大厅预留凭据（cookie）编号。该值由引擎在收到大厅预留时写入，通常不需要手动修改；清零等价于清除预留状态。',
    defaultValue: '0',
    exampleCommand: 'sm_cvar sv_cookie 0',
    source: engineCvarSource,
    verifiedOnServer: true,
    range: {
      min: 0,
      max: 2147483647,
      unit: '凭据编号',
      recommended: '保持引擎自动写入，不要手动改',
      lowerEffect: '0 表示当前没有大厅预留凭据。',
      higherEffect: '非 0 表示该服务器已被某个大厅预留，数值即凭据编号。',
    },
    warning:
      '日常不需要手动设置。想解除预留请用 sv_force_unreserved 或插件的 sm_unreserve 指令，直接改这个数值可能让大厅状态与实际不一致。',
  },
  {
    name: 'sv_maxplayers',
    displayName: '最大人类玩家数',
    category: '基础与连接',
    type: '数值',
    description:
      '服务器允许加入的最大人类玩家数（引擎原文：Max human players）。原版引擎默认 -1，即沿用启动参数 maxplayers；超过 4 人必须配合多人数扩展（如 l4dtoolz）和启动参数一起改。',
    defaultValue: '-1（引擎默认；当前服务器为 16）',
    exampleCommand: 'sm_cvar sv_maxplayers 16',
    source: engineCvarSource,
    verifiedOnServer: true,
    range: {
      min: -1,
      max: 31,
      unit: '人',
      recommended: '不超过启动项 maxplayers 和扩展上限',
      hardLimit: true,
      lowerEffect: '降低可进服人数，满员后新玩家会被拒绝。',
      higherEffect: '提高可进服人数，但超过服务器承载会出现卡顿、实体溢出或扩展报错。',
    },
    warning: '这个变量不能单独突破原版上限；需要配套扩展与启动参数，否则填了也不会生效。',
  },
  {
    name: 'sv_visiblemaxplayers',
    displayName: '服务器列表显示人数',
    category: '基础与连接',
    type: '数值',
    description:
      '覆盖向潜在客户端报告的玩家上限（引擎原文：Overrides the max players reported to prospective clients），也就是服务器浏览器里显示的人数。填 -1 表示使用真实值，它不改变实际能进服的人数。',
    defaultValue: '-1（引擎默认；当前服务器为 16）',
    exampleCommand: 'sm_cvar sv_visiblemaxplayers 16',
    source: engineCvarSource,
    verifiedOnServer: true,
    range: {
      min: -1,
      max: 31,
      unit: '人',
      recommended: '与 sv_maxplayers 保持一致',
      lowerEffect: '列表显示的空位更少，玩家会误以为快满员。',
      higherEffect: '列表显示更多空位，但真实进服仍受 sv_maxplayers 和扩展限制。',
    },
  },
  {
    name: 'sv_maxrate',
    displayName: '单客户端最大网络速率',
    category: '基础与连接',
    type: '数值',
    description:
      '单个客户端允许的最大下行带宽（引擎原文：Max bandwidth rate allowed on server, 0 == unlimited）。0 表示不限制。',
    defaultValue: '0',
    exampleCommand: 'sm_cvar sv_maxrate 0',
    source: engineCvarSource,
    verifiedOnServer: true,
    range: {
      min: 0,
      max: 30000,
      unit: '字节/秒',
      recommended: '0 或 20000-30000',
      hardLimit: true,
      lowerEffect: '降低网络占用，但玩家可能高延迟、掉包、动作不同步。',
      higherEffect: '允许更高流量，网络好的玩家更流畅，但服务器上行压力更大。',
    },
    warning: '引擎把这个变量的上限硬编码为 30000，填更大数值会被截断，不会生效。',
  },
  {
    name: 'sv_minrate',
    displayName: '单客户端最低网络速率',
    category: '基础与连接',
    type: '数值',
    description:
      '服务器要求客户端达到的最低带宽（引擎原文：Min bandwidth rate allowed on server, 0 == unlimited）。低于该值的客户端会被判定网络不足。',
    defaultValue: '5000（引擎默认；当前服务器为 60000）',
    exampleCommand: 'sm_cvar sv_minrate 60000',
    source: engineCvarSource,
    verifiedOnServer: true,
    range: {
      min: 0,
      max: 500000,
      unit: '字节/秒',
      recommended: '30000-100000',
      lowerEffect: '对弱网更宽容，但可能出现更多动作不同步。',
      higherEffect: '减少低速率造成的异常，但弱网玩家更容易卡顿或被踢。',
    },
    warning: '引擎未给这个变量设硬上限，但填得远高于玩家实际带宽会导致持续掉包。',
  },
  {
    name: 'sv_maxcmdrate',
    displayName: '客户端最高指令速率',
    category: '基础与连接',
    type: '数值',
    description:
      '当 sv_mincmdrate 大于 0 时，这个变量决定允许客户端设置的最高 cl_cmdrate（引擎原文：(If sv_mincmdrate is > 0), this sets the maximum value for cl_cmdrate），即客户端每秒向服务器发指令包的上限。',
    defaultValue: '40（引擎默认；当前服务器为 60）',
    exampleCommand: 'sm_cvar sv_maxcmdrate 60',
    source: engineCvarSource,
    verifiedOnServer: true,
    range: {
      min: 10,
      max: 128,
      unit: '次/秒',
      recommended: '与 tickrate 接近，常用 60',
      lowerEffect: '降低上传和服务器处理压力，但高 ping 玩家射击与移动更不跟手。',
      higherEffect: '操作反馈更细，但占用更多 CPU 和带宽。',
    },
    warning: '只有 sv_mincmdrate 大于 0 时才起作用；两者都为 0 时由客户端自行决定。',
  },
  {
    name: 'sv_mincmdrate',
    displayName: '客户端最低指令速率',
    category: '基础与连接',
    type: '数值',
    description:
      '设置客户端 cl_cmdrate 的下限（引擎原文：This sets the minimum value for cl_cmdrate. 0 == unlimited）。0 表示不限制。',
    defaultValue: '0（引擎默认；当前服务器为 60）',
    exampleCommand: 'sm_cvar sv_mincmdrate 60',
    source: engineCvarSource,
    verifiedOnServer: true,
    range: {
      min: 0,
      max: 128,
      unit: '次/秒',
      recommended: '30-60',
      lowerEffect: '弱网玩家更容易保持连接，但动作更新更粗。',
      higherEffect: '提高同步精度，但弱网玩家更容易掉包或被判定网络异常。',
    },
    warning: '设为 0 表示不设下限；设得过高会把弱网玩家直接卡掉。',
  },
  {
    name: 'sv_downloadurl',
    displayName: '自定义资源下载地址',
    category: '基础与连接',
    type: '文本',
    description:
      '客户端下载缺失文件时使用的地址（引擎原文：Location from which clients can download missing files）。填 HTTP/CDN 地址后可绕过游戏服上传带宽瓶颈，地址必须能公网访问。',
    defaultValue: '空',
    exampleCommand: 'sm_cvar sv_downloadurl "https://example.com/l4d2/"',
    source: engineCvarSource,
    verifiedOnServer: true,
    values: [
      { value: '""', label: '关闭', effect: '客户端直接向游戏服请求缺失资源。', tone: 'neutral' },
      {
        value: '"https://你的域名/l4d2/"',
        label: '启用',
        effect: '客户端从该地址下载资源；服务器带宽压力会明显降低。',
        tone: 'good',
      },
    ],
    warning: '地址末尾通常要保留斜杠，且目录结构需要与 left4dead2 资源路径一致。',
  },
  {
    name: 'sv_voiceenable',
    displayName: '语音系统',
    category: '基础与连接',
    type: '开关',
    description:
      '控制服务器是否处理玩家语音。服务器 help 文本为空（仅 archive notify 标记），此项含义来自变量名与引擎行为：关闭后语音数据不再被服务器转发。带 archive 标记，表示该值会写入 cfg。',
    defaultValue: '1',
    exampleCommand: 'sm_cvar sv_voiceenable 1',
    source: engineCvarSource,
    verifiedOnServer: true,
    values: booleanValues('服务器转发玩家语音，队伍内可以互相通话。', '服务器不转发语音，所有玩家都听不到队友声音。'),
    warning: '这是服务器级别的总开关，不是单个玩家的语音设置；关掉后客户端怎么调都听不到队友。',
  },
  {
    name: 'sv_alltalk',
    displayName: '全员语音',
    category: '基础与连接',
    type: '开关',
    description:
      '所有玩家能听见其他所有玩家的语音，不受队伍限制（引擎原文：Players can hear all other players\' voice communication, no team restrictions）。带 game notify 标记。',
    defaultValue: '0',
    exampleCommand: 'sm_cvar sv_alltalk 0',
    source: engineCvarSource,
    verifiedOnServer: true,
    values: booleanValues(
      '所有人都能听见所有人的语音，适合娱乐服或特殊模式。',
      '只在队伍内通信，适合对抗和正式模式。',
    ),
  },
  {
    name: 'sv_cheats',
    displayName: '服务器作弊指令',
    category: '基础与连接',
    type: '开关',
    description:
      '允许在服务器上使用作弊指令（引擎原文：Allow cheats on server）。带 notify replicated 标记，改动立即同步给已连接的客户端。所有带 cheat 标记的变量（如 sv_infinite_ammo、z_common_limit、director_no_specials）都必须先把它设为 1 才能生效。',
    defaultValue: '0',
    exampleCommand: 'sm_cvar sv_cheats 1',
    source: engineCvarSource,
    verifiedOnServer: true,
    values: booleanValues(
      '允许使用带 cheat 标记的指令和变量，只建议在私人测试服短暂开启。',
      '禁止作弊类指令，正式服应保持关闭。',
    ),
    warning: '开启后大量平衡性判定会改变，且客户端可执行作弊指令；正式服务器不要常开。',
  },
  {
    name: 'z_difficulty',
    displayName: '游戏难度',
    category: '游戏与对抗',
    type: '枚举',
    description:
      '当前游戏难度（引擎原文：Difficulty of the current game (Easy, Normal, Hard, Impossible)）。带 game replicated 标记。只接受这四个英文单词，大小写敏感，写成中文或小写都不会生效。',
    defaultValue: 'Normal（引擎默认；当前服务器为 Normal）',
    exampleCommand: 'sm_cvar z_difficulty Hard',
    source: engineCvarSource,
    verifiedOnServer: true,
    values: [
      { value: 'Easy', label: '简单', effect: '普通感染者与特殊感染者伤害最低，资源更宽松。', tone: 'good' },
      { value: 'Normal', label: '普通', effect: '标准伤害和资源，适合大多数玩家。' },
      { value: 'Hard', label: '高级', effect: '伤害提高、补给减少，队友伤害更容易造成危险。', tone: 'warning' },
      { value: 'Impossible', label: '专家', effect: '伤害最高，倒地和死亡惩罚最重。', tone: 'danger' },
    ],
    warning: '部分插件会读取当前难度；修改后最好配合换图或重启地图流程。',
  },
  {
    name: 'z_common_limit',
    displayName: '普通感染者同时上限',
    category: '游戏与对抗',
    type: '数值',
    description:
      '同时存在的普通感染者数量上限（引擎原文：How many common infecteds we can have at once）。带 cheat 标记。',
    defaultValue: '30',
    exampleCommand: 'sm_cvar z_common_limit 30',
    source: engineCvarSource,
    verifiedOnServer: true,
    range: {
      min: 0,
      max: 100,
      unit: '只',
      recommended: '20-40',
      lowerEffect: '尸潮更稀薄，压力更低，但画面和节奏也会变空。',
      higherEffect: '普通感染者更多、更密集，服务器 CPU 和网络压力明显增加。',
    },
  },
  {
    name: 'z_mob_spawn_min_size',
    displayName: '小尸潮最小数量',
    category: '游戏与对抗',
    type: '数值',
    description:
      '尸潮（mob）一次生成数量的下限，实际数量会在下限与 z_mob_spawn_max_size 之间取值。服务器 help 文本未给出说明，此项含义来自引擎行为，带 cheat 标记。',
    defaultValue: '10',
    exampleCommand: 'sm_cvar z_mob_spawn_min_size 10',
    source: engineCvarSource,
    verifiedOnServer: true,
    range: {
      min: 0,
      max: 100,
      unit: '只',
      recommended: '10-20',
      lowerEffect: '每次尸潮规模更小，整体难度下降。',
      higherEffect: '小尸潮的最低规模变大，持续战斗压力增加。',
    },
  },
  {
    name: 'z_mob_spawn_max_size',
    displayName: '小尸潮最大数量',
    category: '游戏与对抗',
    type: '数值',
    description:
      '尸潮（mob）一次生成数量的上限，实际数量会在 z_mob_spawn_min_size 与上限之间取值。服务器 help 文本未给出说明，此项含义来自引擎行为，带 cheat 标记。',
    defaultValue: '30',
    exampleCommand: 'sm_cvar z_mob_spawn_max_size 30',
    source: engineCvarSource,
    verifiedOnServer: true,
    range: {
      min: 0,
      max: 100,
      unit: '只',
      recommended: '20-40',
      lowerEffect: '尸潮峰值更低，适合低配服或休闲服。',
      higherEffect: '尸潮峰值更高，但可能造成瞬间卡顿和实体数量压力。',
    },
  },
  {
    name: 'z_special_spawn_interval',
    displayName: '特殊感染者生成间隔',
    category: '游戏与对抗',
    type: '数值',
    description:
      '特殊感染者的生成间隔（引擎原文：Interval for spawning special zombies）。带 cheat 标记。',
    defaultValue: '45',
    exampleCommand: 'sm_cvar z_special_spawn_interval 45',
    source: engineCvarSource,
    verifiedOnServer: true,
    range: {
      min: 1,
      max: 300,
      unit: '秒',
      recommended: '30-60',
      lowerEffect: '特殊感染者更频繁，玩家压力更大，可能连续被控。',
      higherEffect: '特殊感染者更少，节奏更轻松，但对抗服会变得偏空。',
    },
  },
  {
    name: 'director_no_specials',
    displayName: '禁止特殊感染者',
    category: '游戏与对抗',
    type: '开关',
    description:
      '关闭导演对特殊感染者的生成（引擎原文：Disable PZ spawns）。带 cheat 标记。',
    defaultValue: '0',
    exampleCommand: 'sm_cvar director_no_specials 0',
    source: engineCvarSource,
    verifiedOnServer: true,
    values: booleanValues(
      '停止导演新增特殊感染者，适合测试、跑图或娱乐模式。',
      '恢复导演正常生成特殊感染者。',
    ),
    warning: '只影响导演生成；地图脚本或插件强制刷出的特感不受此变量控制。',
  },
  {
    name: 'director_no_mobs',
    displayName: '禁止普通尸潮',
    category: '游戏与对抗',
    type: '开关',
    description:
      '关闭导演触发的普通感染者尸潮冲锋（引擎原文：Disable mob rushes）。带 cheat 标记。',
    defaultValue: '0',
    exampleCommand: 'sm_cvar director_no_mobs 0',
    source: engineCvarSource,
    verifiedOnServer: true,
    values: booleanValues(
      '关闭普通尸潮，玩家只需要面对已存在敌人和特殊感染者。',
      '恢复普通尸潮生成。',
    ),
    warning: '和地图脚本强制生成的敌人不冲突；它不是“清空所有僵尸”。',
  },
  {
    name: 'director_ready_duration',
    displayName: '开局准备时间',
    category: '游戏与对抗',
    type: '数值',
    description:
      '非 0 时，生还者队伍在正式出发前拥有这么多秒的准备时间（引擎原文：If nonzero, survivor team has this amount of time to get ready before starting out）。0 表示跳过准备阶段。带 cheat 标记。',
    defaultValue: '60',
    exampleCommand: 'sm_cvar director_ready_duration 60',
    source: engineCvarSource,
    verifiedOnServer: true,
    range: {
      min: 0,
      max: 180,
      unit: '秒',
      recommended: '20-60',
      lowerEffect: '更快开局，但玩家准备装备和集合的时间更少。',
      higherEffect: '准备时间更长，容易让熟练玩家觉得拖沓。',
    },
  },
  {
    name: 'survivor_revive_duration',
    displayName: '扶起倒地队友耗时',
    category: '游戏与对抗',
    type: '数值',
    description:
      '把倒地的生还者扶起来所需的时间。服务器 help 文本为空（仅 game cheat 标记），此项含义来自引擎行为。',
    defaultValue: '5',
    exampleCommand: 'sm_cvar survivor_revive_duration 5',
    source: engineCvarSource,
    verifiedOnServer: true,
    range: {
      min: 0,
      max: 30,
      unit: '秒',
      recommended: '4-6',
      lowerEffect: '扶人更快，整体难度下降，特感控场价值降低。',
      higherEffect: '扶人更慢，救援窗口更危险，容易被连续控住。',
    },
  },
  {
    name: 'survivor_revive_health',
    displayName: '被扶起后的临时生命值',
    category: '游戏与对抗',
    type: '数值',
    description:
      '被救起时获得的临时生命值数量（引擎原文：How much temp health you get revived with）。注意是临时生命（虚血），会随时间衰减，不是永久血量。',
    defaultValue: '30',
    exampleCommand: 'sm_cvar survivor_revive_health 30',
    source: engineCvarSource,
    verifiedOnServer: true,
    range: {
      min: 1,
      max: 100,
      unit: '生命值',
      recommended: '30-50',
      lowerEffect: '被扶起后更脆弱，下一次倒地风险更高。',
      higherEffect: '被扶起后更耐打，救援收益更高。',
    },
  },
  {
    name: 'survivor_max_incapacitated_count',
    displayName: '最大倒地次数',
    category: '游戏与对抗',
    type: '数值',
    description:
      '在两次使用医疗包之间，你可以被击倒而不是直接死亡的次数（引擎原文：How many times you can be incapacitated instead of killed between health kits）。',
    defaultValue: '2',
    exampleCommand: 'sm_cvar survivor_max_incapacitated_count 2',
    source: engineCvarSource,
    verifiedOnServer: true,
    range: {
      min: 0,
      max: 10,
      unit: '次',
      recommended: '2-3',
      lowerEffect: '更早死亡，容错更低；0 通常会非常严苛。',
      higherEffect: '允许更多倒地，玩家更耐打，整体难度下降。',
    },
  },
  {
    name: 'sv_infinite_ammo',
    displayName: '无限弹药',
    category: '游戏与对抗',
    type: '枚举',
    description:
      "开启后玩家手持的武器永远不会耗尽弹药（引擎原文：Player's active weapon will never run out of ammo）。带 cheat 标记，需配合 sv_cheats 1。",
    defaultValue: '0',
    exampleCommand: 'sm_cvar sv_infinite_ammo 1',
    source: engineCvarSource,
    verifiedOnServer: true,
    values: [
      { value: '0', label: '关闭', effect: '弹药按正常规则消耗。', tone: 'neutral' },
      { value: '1', label: '弹药永不耗尽', effect: '弹匣与备弹都不减少，连换弹都不需要。', tone: 'warning' },
      {
        value: '2',
        label: '备弹不减（仍需换弹）',
        effect: '备弹数量不减少，但弹匣打空后仍要执行换弹动作。',
        tone: 'warning',
      },
    ],
    warning: '属于作弊功能，正式对抗服不要开启。',
  },
  {
    name: 'sb_all_bot_game',
    displayName: '允许全 AI 服务器',
    category: '游戏与对抗',
    type: '开关',
    description:
      '允许一台只有 AI、没有真人玩家的服务器（引擎原文：Allow a server of nothing but bots）。带 cheat 标记。',
    defaultValue: '0',
    exampleCommand: 'sm_cvar sb_all_bot_game 1',
    source: engineCvarSource,
    verifiedOnServer: true,
    values: booleanValues(
      '即使没有真人，地图和 AI 仍可继续运行，适合挂机测试或保持房间状态。',
      '没有真人时停止正常游戏推进。',
    ),
    warning: '该变量带 cheat 标记，修改前通常需要先开启 sv_cheats。',
  },
  {
    name: 'allow_all_bot_survivor_team',
    displayName: '全机器人生还者队伍',
    category: '游戏与对抗',
    type: '开关',
    description:
      '允许生还者队伍全部由 AI 组成（引擎原文：Allow a survivor team of nothing but bots. Human players are still required for the game to be active）。注意后半句：仍然必须有真人玩家，游戏才会真正激活。',
    defaultValue: '0',
    exampleCommand: 'sm_cvar allow_all_bot_survivor_team 1',
    source: engineCvarSource,
    verifiedOnServer: true,
    values: booleanValues(
      '允许全 AI 生还者队伍，但服务器仍需至少一名真人才能让游戏激活。',
      '生还者队伍必须包含真人，按原版规则运行。',
    ),
    warning:
      '不能用来实现「零真人自动跑图」。想要无真人推进地图，需要的是 sb_all_bot_game（且带 cheat 标记），不是这个变量。',
  },
  {
    name: 'l4d2_afk_kick',
    displayName: 'AFK 自动踢人总开关',
    category: '插件扩展',
    type: '枚举',
    description:
      '插件原文：启用普通玩家或管理员闲置超时踢出？(总开关, 禁用后指令开关也不可用) 0=禁用, 1=启用(管理员免疫), 2=启用(包括管理员)。它是总开关，设为 0 后连 !timer 指令也失效。',
    defaultValue: '1（插件默认；当前服务器为 2）',
    exampleCommand: 'sm_cvar l4d2_afk_kick 2',
    source: serverCvarSource,
    verifiedOnServer: true,
    values: [
      { value: '0', label: '关闭', effect: '不自动踢出任何闲置玩家，且 !timer 指令开关失效。', tone: 'neutral' },
      { value: '1', label: '管理员免疫', effect: '普通玩家超时会被踢，管理员不受影响。', tone: 'good' },
      { value: '2', label: '包括管理员', effect: '普通玩家和管理员都按各自超时时间被踢。', tone: 'warning' },
    ],
    warning: '设为 0 会连指令层面的开关一起禁用，此时只能用这个 ConVar 重新打开。',
  },
  {
    name: 'l4d2_afk_kick_switch',
    displayName: 'AFK 踢人默认状态',
    category: '插件扩展',
    type: '开关',
    description:
      '插件原文：设置默认开启或关闭自动踢出玩家或管理员？(输入指令 !timer 关闭或开启) 0=关闭, 1=开启。它决定插件运行时的初始状态，游戏中仍可用 !timer 临时切换。',
    defaultValue: '1',
    exampleCommand: 'sm_cvar l4d2_afk_kick_switch 1',
    source: serverCvarSource,
    verifiedOnServer: true,
    values: booleanValues(
      '插件运行时默认开启 AFK 检测。',
      '插件运行时默认关闭 AFK 检测，仍可用 !timer 临时开启。',
    ),
    warning: '总开关 l4d2_afk_kick 为 0 时，这个变量和 !timer 指令都不会生效。',
  },
  {
    name: 'l4d2_afk_player_kick_time',
    displayName: '普通玩家 AFK 超时',
    category: '插件扩展',
    type: '数值',
    description:
      '插件原文：设置普通玩家闲置超时踢出的时间/秒(不建议低于 300 秒)。',
    defaultValue: '300（插件默认；当前服务器为 600）',
    exampleCommand: 'sm_cvar l4d2_afk_player_kick_time 600',
    source: serverCvarSource,
    verifiedOnServer: true,
    range: {
      min: 300,
      max: 7200,
      unit: '秒',
      recommended: '300-900（5-15 分钟）',
      lowerEffect: '更容易踢掉挂机玩家，但正常观战、聊天或短暂离开也更容易误伤。',
      higherEffect: '更宽容，空位可能被挂机玩家占用更久。',
    },
    warning: '插件明确不建议低于 300 秒，设太小会频繁误踢正常玩家。',
  },
  {
    name: 'l4d2_afk_player_kick_time_admin',
    displayName: '管理员 AFK 超时',
    category: '插件扩展',
    type: '数值',
    description:
      '插件原文：设置管理员闲置超时踢出的时间/秒(不建议低于 300 秒)。仅当总开关 l4d2_afk_kick 设为 2 时才会真正踢管理员。',
    defaultValue: '500（插件默认；当前服务器为 1200）',
    exampleCommand: 'sm_cvar l4d2_afk_player_kick_time_admin 1200',
    source: serverCvarSource,
    verifiedOnServer: true,
    range: {
      min: 300,
      max: 14400,
      unit: '秒',
      recommended: '900-1800',
      lowerEffect: '管理员挂机更早被踢，可能影响值班管理。',
      higherEffect: '管理员可挂机更久，适合长时间值守。',
    },
    warning: '总开关为 0 或 1 时，管理员都不会被踢，这个数值不生效。',
  },
  {
    name: 'l4d2_tank_health',
    displayName: 'Tank 每人额外血量',
    category: '插件扩展',
    type: '数值',
    description:
      '插件原文：设置每多一个生还者坦克所增加的血量。配合 l4d2_tank_Switch 一起生效。',
    defaultValue: '2500（插件默认；当前服务器为 1000）',
    exampleCommand: 'sm_cvar l4d2_tank_health 1000',
    source: serverCvarSource,
    verifiedOnServer: true,
    range: {
      min: 0,
      max: 10000,
      unit: '生命值',
      recommended: '500-1500',
      lowerEffect: '多人生还者时 Tank 更脆，击杀更快。',
      higherEffect: 'Tank 血量随人数增长更快，战斗时间更长。',
    },
    warning: 'l4d2_tank_Switch 为 0 时这个数值不生效，所有人数的 Tank 都只用基础血量。',
  },
  {
    name: 'l4d2_tank_minimum',
    displayName: 'Tank 基础血量',
    category: '插件扩展',
    type: '数值',
    description:
      '插件原文：设置坦克的基础生命值(四名生还者或以内时)。注意括号里的限定——四名或更少生还者时 Tank 用这个基础值；超过四人后由 l4d2_tank_health 按人数叠加。',
    defaultValue: '4000',
    exampleCommand: 'sm_cvar l4d2_tank_minimum 4000',
    source: serverCvarSource,
    verifiedOnServer: true,
    range: {
      min: 1,
      max: 50000,
      unit: '生命值',
      recommended: '3000-8000',
      lowerEffect: 'Tank 更脆，战斗时间明显缩短。',
      higherEffect: 'Tank 更耐打，人少时也可能拖很久。',
    },
    warning: '它不是「血量下限」而是「四人及以内的基础血量」；名字里的 minimum 容易被误解成保底值。',
  },
  {
    name: 'l4d2_tank_Switch',
    displayName: 'Tank 人数加成开关',
    category: '插件扩展',
    type: '开关',
    description:
      '插件原文：启用坦克出现时根据生还者人数而增加血量？0=禁用, 1=启用。开启后 Tank 血量 = 基础血量 +（生还者人数 - 4）× 每人额外血量。',
    defaultValue: '1',
    exampleCommand: 'sm_cvar l4d2_tank_Switch 1',
    source: serverCvarSource,
    verifiedOnServer: true,
    values: booleanValues(
      '人数超过四名时，Tank 按 l4d2_tank_health 逐人加血。',
      '忽略人数加成，所有人数的 Tank 都只用 l4d2_tank_minimum 的基础血量。',
    ),
  },
  {
    name: 'l4d2_witch_health',
    displayName: 'Witch 每人额外血量',
    category: '插件扩展',
    type: '数值',
    description:
      '插件原文：设置每多一个生还者女巫所增加的血量。只在 l4d2_witch_Switch 开启时生效。',
    defaultValue: '200（插件默认；当前服务器为 250）',
    exampleCommand: 'sm_cvar l4d2_witch_health 250',
    source: serverCvarSource,
    verifiedOnServer: true,
    range: {
      min: 0,
      max: 5000,
      unit: '生命值',
      recommended: '150-500',
      lowerEffect: '多人时 Witch 更容易被快速击杀。',
      higherEffect: 'Witch 更耐打，惊扰后的风险更高。',
    },
    warning: '当前服务器 l4d2_witch_Switch 为 0，所以这个数值暂时不生效。',
  },
  {
    name: 'l4d2_witch_minimum',
    displayName: 'Witch 基础血量',
    category: '插件扩展',
    type: '数值',
    description:
      '插件原文：设置女巫的基础生命值(四名生还者或以内时)。四名或更少生还者时 Witch 用这个基础值；超过四人后由 l4d2_witch_health 按人数叠加。',
    defaultValue: '1000',
    exampleCommand: 'sm_cvar l4d2_witch_minimum 1000',
    source: serverCvarSource,
    verifiedOnServer: true,
    range: {
      min: 1,
      max: 50000,
      unit: '生命值',
      recommended: '800-2000',
      lowerEffect: 'Witch 更容易被一两个弹匣击杀。',
      higherEffect: 'Witch 更耐打，失误代价更高。',
    },
    warning: '名字里的 minimum 指「四人及以内的基础血量」，不是任何情况下的保底值。',
  },
  {
    name: 'l4d2_witch_Switch',
    displayName: 'Witch 人数加成开关',
    category: '插件扩展',
    type: '开关',
    description:
      '插件原文：启用女巫出现时根据生还者人数而增加血量？0=禁用, 1=启用。插件默认 1，但当前服务器实际为 0，即人数加成关闭。',
    defaultValue: '1（插件默认；当前服务器为 0）',
    exampleCommand: 'sm_cvar l4d2_witch_Switch 1',
    source: serverCvarSource,
    verifiedOnServer: true,
    values: booleanValues(
      '人数超过四名时，Witch 按 l4d2_witch_health 逐人加血。',
      '忽略人数加成，所有人数的 Witch 都只用 l4d2_witch_minimum 的基础血量。',
    ),
  },
  {
    name: 'l4d2_vote_Percent',
    displayName: '投票通过百分比',
    category: '插件扩展',
    type: '数值',
    description:
      '插件原文：设置通过投票所需的百分比(1/100)。按参与者人数的百分比计算，不是按全服总人数。',
    defaultValue: '60',
    exampleCommand: 'sm_cvar l4d2_vote_Percent 60',
    source: serverCvarSource,
    verifiedOnServer: true,
    range: {
      min: 1,
      max: 100,
      unit: '%',
      recommended: '60-80',
      lowerEffect: '更容易通过，少数玩家也能推动换图或踢人。',
      higherEffect: '更难通过，可减少小团体滥用，但正常投票也容易失败。',
    },
  },
  {
    name: 'l4d2_vote_menuTime',
    displayName: '投票菜单显示时间',
    category: '插件扩展',
    type: '数值',
    description:
      '插件原文：设置同意或反对菜单的显示时间/秒。超时未选择视为放弃本次投票。',
    defaultValue: '25（插件默认；当前服务器为 20）',
    exampleCommand: 'sm_cvar l4d2_vote_menuTime 25',
    source: serverCvarSource,
    verifiedOnServer: true,
    range: {
      min: 5,
      max: 180,
      unit: '秒',
      recommended: '20-40',
      lowerEffect: '投票节奏快，但玩家可能来不及阅读和选择。',
      higherEffect: '给玩家更多时间，但战斗中容易打断节奏。',
    },
  },
  {
    name: 'l4d2_getitem_allow_oxygentank',
    displayName: '允许获取氧气瓶',
    category: '插件扩展',
    type: '开关',
    description:
      '插件原文：默认是否允许获取氧气瓶（0：关闭，1：开启；重启服务器或重载插件后生效）。控制物品获取插件的可获取列表里是否包含氧气瓶。',
    defaultValue: '0',
    exampleCommand: 'sm_cvar l4d2_getitem_allow_oxygentank 1',
    source: serverCvarSource,
    verifiedOnServer: true,
    values: booleanValues(
      '氧气瓶可以被获取，可能影响爆炸物和燃烧场景平衡。',
      '插件不会把氧气瓶加入可获取物品。',
    ),
    warning: '插件自带说明：重启服务器或重载插件后生效，改完不会立即作用于当前回合。',
  },
  {
    name: 'l4d2_getitem_allow_propanetank',
    displayName: '允许获取煤气罐',
    category: '插件扩展',
    type: '开关',
    description:
      '插件原文：默认是否允许获取煤气罐（0：关闭，1：开启；重启服务器或重载插件后生效）。控制物品获取插件的可获取列表里是否包含煤气罐。',
    defaultValue: '0',
    exampleCommand: 'sm_cvar l4d2_getitem_allow_propanetank 1',
    source: serverCvarSource,
    verifiedOnServer: true,
    values: booleanValues(
      '煤气罐可以被获取，会显著增加爆炸伤害和场景破坏。',
      '插件不会把煤气罐加入可获取物品。',
    ),
    warning: '插件自带说明：重启服务器或重载插件后生效，改完不会立即作用于当前回合。',
  },
  {
    name: 'l4d2_weapon_drop_glow_fix_enable',
    displayName: '武器掉落发光修复',
    category: '插件扩展',
    type: '开关',
    description:
      '插件原文：启用物品掉落发光修复. 0=禁用, 1=启用。开启后修复掉落武器不发光或发光异常的显示问题。',
    defaultValue: '1',
    exampleCommand: 'sm_cvar l4d2_weapon_drop_glow_fix_enable 1',
    source: serverCvarSource,
    verifiedOnServer: true,
    values: booleanValues(
      '修复掉落武器发光异常，便于玩家寻找。',
      '关闭修复，按原版掉落发光行为运行。',
    ),
  },
  {
    name: 'l4d2_melee_spawn',
    displayName: '解锁近战武器列表',
    category: '插件扩展',
    type: '文本',
    description:
      '插件原文：Melee weapon list for unlock, use \';\' to separate between names, e.g: pitchfork;shovel. Empty for no change。即要额外解锁生成的近战武器名单，多个名称用分号分隔，留空表示不修改地图原本的近战生成。',
    defaultValue: '空',
    exampleCommand: 'sm_cvar l4d2_melee_spawn "pitchfork;shovel"',
    source: serverCvarSource,
    verifiedOnServer: true,
    values: [
      { value: '""', label: '不修改', effect: '按地图原本的近战武器生成。', tone: 'neutral' },
      {
        value: '"pitchfork;shovel"',
        label: '加入干草叉和铲子',
        effect: '地图会按插件逻辑尝试加入这两类近战武器。',
        tone: 'good',
      },
    ],
    warning: '武器名称必须与插件支持列表一致；拼错通常只会忽略该项，不会报错。',
  },
];

export interface CvarPresetValue {
  value: string;
  label: string;
}

/**
 * 数值型 cvar 的常用取值预设（纯 UX 元数据，不属于引擎事实）。
 * 集中维护在这里，避免反复改动已经审计过的 range 块。
 */
export const cvarRangePresets: Record<string, CvarPresetValue[]> = {
  sv_cookie: [
    { value: '0', label: '清除预留' },
    { value: '-1', label: '不限制' },
  ],
  sv_maxplayers: [
    { value: '4', label: '原版' },
    { value: '8', label: '常用' },
    { value: '16', label: '多人生存' },
    { value: '31', label: '上限' },
  ],
  sv_visiblemaxplayers: [
    { value: '-1', label: '跟随实际' },
    { value: '8', label: '常用' },
    { value: '31', label: '上限' },
  ],
  sv_maxrate: [
    { value: '0', label: '不限速' },
    { value: '20000', label: '中等' },
    { value: '30000', label: '上限' },
  ],
  sv_minrate: [
    { value: '5000', label: '默认' },
    { value: '30000', label: '常用' },
    { value: '100000', label: '高带宽' },
  ],
  sv_maxcmdrate: [
    { value: '30', label: '30 tick' },
    { value: '60', label: '60 tick' },
    { value: '100', label: '100 tick' },
    { value: '128', label: '128 tick' },
  ],
  sv_mincmdrate: [
    { value: '0', label: '不限制' },
    { value: '30', label: '常用' },
    { value: '60', label: '高要求' },
  ],
  z_common_limit: [
    { value: '15', label: '轻松' },
    { value: '20', label: '常用' },
    { value: '30', label: '默认' },
    { value: '40', label: '高压' },
  ],
  z_mob_spawn_min_size: [
    { value: '5', label: '小批量' },
    { value: '10', label: '默认' },
    { value: '20', label: '大批量' },
  ],
  z_mob_spawn_max_size: [
    { value: '20', label: '轻松' },
    { value: '30', label: '默认' },
    { value: '40', label: '尸潮' },
  ],
  z_special_spawn_interval: [
    { value: '30', label: '密集' },
    { value: '45', label: '默认' },
    { value: '60', label: '稀疏' },
  ],
  director_ready_duration: [
    { value: '20', label: '快节奏' },
    { value: '30', label: '常用' },
    { value: '60', label: '默认' },
  ],
  survivor_revive_duration: [
    { value: '3', label: '快速' },
    { value: '5', label: '默认' },
    { value: '10', label: '硬核' },
  ],
  survivor_revive_health: [
    { value: '30', label: '默认' },
    { value: '50', label: '宽松' },
    { value: '100', label: '满血复活' },
  ],
  survivor_max_incapacitated_count: [
    { value: '2', label: '默认' },
    { value: '3', label: '宽松' },
    { value: '5', label: '极宽松' },
  ],
  l4d2_afk_player_kick_time: [
    { value: '600', label: '10 分钟' },
    { value: '900', label: '15 分钟' },
    { value: '1800', label: '30 分钟' },
  ],
  l4d2_afk_player_kick_time_admin: [
    { value: '900', label: '15 分钟' },
    { value: '1800', label: '30 分钟' },
    { value: '3600', label: '60 分钟' },
  ],
  l4d2_tank_health: [
    { value: '500', label: '简单' },
    { value: '1000', label: '常见' },
    { value: '1500', label: '偏高' },
    { value: '2500', label: '默认' },
  ],
  l4d2_tank_minimum: [
    { value: '2000', label: '偏低' },
    { value: '4000', label: '默认' },
    { value: '8000', label: '偏高' },
  ],
  l4d2_witch_health: [
    { value: '100', label: '简单' },
    { value: '200', label: '默认' },
    { value: '500', label: '偏高' },
  ],
  l4d2_witch_minimum: [
    { value: '500', label: '偏低' },
    { value: '1000', label: '默认' },
    { value: '2000', label: '偏高' },
  ],
  l4d2_vote_percent: [
    { value: '50', label: '过半' },
    { value: '60', label: '默认' },
    { value: '66', label: '三分之二' },
    { value: '80', label: '严格' },
  ],
  l4d2_vote_menutime: [
    { value: '15', label: '短' },
    { value: '25', label: '默认' },
    { value: '40', label: '长' },
  ],
};

/** 解析数值型 cvar 的常用取值；没有专门配置时退化为默认值 + 上下限。 */
export const resolveCvarRangePresets = (definition: L4D2CvarDefinition): CvarPresetValue[] => {
  const preset = cvarRangePresets[definition.name.toLowerCase()];
  if (preset?.length) return preset;
  const range = definition.range;
  if (!range) return [];
  const seen = new Set<string>();
  const fallback: CvarPresetValue[] = [];
  const push = (raw: string | number | undefined, label: string) => {
    if (raw === undefined) return;
    const text = String(raw).trim();
    if (!text || seen.has(text)) return;
    seen.add(text);
    fallback.push({ value: text, label });
  };
  push(definition.defaultValue, '默认');
  push(range.min, '最小');
  push(range.max, '最大');
  return fallback;
};

const cvarDefinitionMap = new Map(
  l4d2CvarCatalog.map((definition) => [definition.name.toLowerCase(), definition]),
);

export interface ParsedCvarCommand {
  name: string;
  value: string;
}

export const parseCvarCommand = (command: string): ParsedCvarCommand | null => {
  const tokens = command.trim().match(/"[^"]*"|\S+/g) ?? [];
  if (tokens.length === 0) return null;
  const offset = tokens[0]?.toLowerCase() === 'sm_cvar' ? 1 : 0;
  const rawName = tokens[offset];
  if (!rawName) return null;
  return {
    name: rawName.toLowerCase(),
    value: tokens.slice(offset + 1).join(' ').replace(/^"|"$/g, ''),
  };
};

export const findCvarDefinition = (name: string) =>
  cvarDefinitionMap.get(name.trim().toLowerCase()) ?? null;

export const findCvarDefinitionForCommand = (command: string) => {
  const parsed = parseCvarCommand(command);
  return parsed ? findCvarDefinition(parsed.name) : null;
};
