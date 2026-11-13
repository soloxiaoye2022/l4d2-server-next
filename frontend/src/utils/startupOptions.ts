import {
  startupOptions,
  startupOptionsById,
  type StartupOptionCategory,
  type StartupOptionDefinition,
} from '../data/startupOptions';

export interface StartupEntry {
  id: string;
  definitionId: string | null;
  name: string;
  category: StartupOptionCategory;
  description: string;
  comment: string;
  token: string;
  value: string;
  requiresPlugin?: string;
  integration?: 'sourcetv';
  risk: 'normal' | 'warning' | 'advanced';
}

export interface ParsedStartupCommand {
  executable: string;
  entries: StartupEntry[];
}

let entrySequence = 0;

const nextEntryId = () => {
  entrySequence += 1;
  return `startup-entry-${Date.now()}-${entrySequence}`;
};

const optionsByAlias = new Map<string, StartupOptionDefinition>();
for (const definition of startupOptions) {
  for (const alias of definition.aliases) {
    optionsByAlias.set(alias.toLowerCase(), definition);
  }
}

const looksLikeOption = (value: string) =>
  /^[+-][A-Za-z][A-Za-z0-9_-]*$/.test(value) || value === '--';

export const tokenizeStartupCommand = (command: string): string[] => {
  const tokens: string[] = [];
  let current = '';
  let quote = '';
  let escaped = false;

  for (const character of command) {
    if (escaped) {
      current += character;
      escaped = false;
      continue;
    }
    if (character === '\\') {
      escaped = true;
      continue;
    }
    if (quote) {
      if (character === quote) {
        quote = '';
      } else {
        current += character;
      }
      continue;
    }
    if (character === '"' || character === "'") {
      quote = character;
      continue;
    }
    if (/\s/.test(character)) {
      if (current) {
        tokens.push(current);
        current = '';
      }
      continue;
    }
    current += character;
  }
  if (escaped) current += '\\';
  if (current) tokens.push(current);
  return tokens;
};

export const quoteStartupArgument = (value: string): string => {
  if (!value || !/[\s"'\\]/.test(value)) return value;
  return `"${value.replace(/\\/g, '\\\\').replace(/"/g, '\\"')}"`;
};

const entryFromDefinition = (
  definition: StartupOptionDefinition,
  token = definition.aliases[0] || '',
  value = definition.value?.defaultValue || ''
): StartupEntry => ({
  id: nextEntryId(),
  definitionId: definition.id,
  name: definition.name,
  category: definition.category,
  description: definition.description,
  comment: `${definition.name}：${definition.description}`,
  token,
  value,
  requiresPlugin: definition.requiresPlugin,
  integration: definition.integration,
  risk: definition.risk || 'normal',
});

export const createStartupEntry = (
  definitionId: string,
  token?: string,
  value?: string
): StartupEntry | null => {
  const definition = startupOptionsById.get(definitionId);
  if (!definition) return null;
  return entryFromDefinition(definition, token, value ?? definition.value?.defaultValue ?? '');
};

export const createCustomStartupEntry = (
  token = '',
  value = '',
  name = '自定义参数',
  comment = ''
): StartupEntry => ({
  id: nextEntryId(),
  definitionId: null,
  name,
  category: 'advanced',
  description: comment || '用户自定义启动参数',
  comment: comment || '自定义启动参数',
  token,
  value,
  risk: 'advanced',
});

export const parseStartupCommand = (execStart: string): ParsedStartupCommand => {
  const tokens = tokenizeStartupCommand(execStart);
  if (tokens.length === 0) {
    return { executable: '', entries: [] };
  }

  const executable = tokens[0] || '';
  const entries: StartupEntry[] = [];
  for (let index = 1; index < tokens.length; index += 1) {
    const token = tokens[index] || '';
    const definition = optionsByAlias.get(token.toLowerCase());
    if (definition) {
      if (definition.value) {
        const value = tokens[index + 1] || definition.value.defaultValue;
        if (index + 1 < tokens.length) index += 1;
        entries.push(entryFromDefinition(definition, token, value));
      } else {
        entries.push(entryFromDefinition(definition, token, ''));
      }
      continue;
    }

    let value = '';
    const next = tokens[index + 1];
    if (next && !looksLikeOption(next)) {
      value = next;
      index += 1;
    }
    entries.push(
      createCustomStartupEntry(
        token,
        value,
        token ? `自定义参数 ${token}` : '自定义参数',
        token ? `自定义启动项 ${token}` : '自定义启动项'
      )
    );
  }
  return { executable, entries };
};

export const serializeStartupCommand = (executable: string, entries: StartupEntry[]): string => {
  const tokens = [executable.trim(), ...entries.flatMap((entry) => {
    const result = [entry.token.trim()];
    if (entry.value !== '') result.push(entry.value);
    return result;
  })].filter(Boolean);
  return tokens.map(quoteStartupArgument).join(' ');
};

export const buildStartupComments = (entries: StartupEntry[]): string[] => {
  const comments = ['启动项说明（由 L4D2 Manager 自动生成）'];
  for (const entry of entries) {
    const detail = entry.definitionId
      ? entry.comment.trim() || `${entry.name}：${entry.description}`
      : `${entry.name}：${entry.comment.trim() || entry.description}`;
    const value = entry.value ? ` 当前值：${entry.value}` : '';
    comments.push(`${detail}${value}`);
  }
  return comments;
};

export const hydrateStartupComments = (
  entries: StartupEntry[],
  comments: string[] | undefined
): StartupEntry[] => {
  if (!comments || comments.length !== entries.length + 1) return entries;
  return entries.map((entry, index) => {
    let comment = comments[index + 1]?.trim() || entry.comment;
    const valueSuffix = entry.value ? ` 当前值：${entry.value}` : '';
    if (valueSuffix && comment.endsWith(valueSuffix)) {
      comment = comment.slice(0, -valueSuffix.length);
    }
    if (!entry.definitionId) {
      const separator = comment.indexOf('：');
      if (separator > 0) {
        return {
          ...entry,
          name: comment.slice(0, separator).trim() || entry.name,
          comment: comment.slice(separator + 1).trim() || entry.comment,
          description: comment.slice(separator + 1).trim() || entry.description,
        };
      }
    }
    return { ...entry, comment };
  });
};

export const startupDefinitionForEntry = (entry: StartupEntry) =>
  entry.definitionId ? startupOptionsById.get(entry.definitionId) : undefined;

export const startupEntrySupportsValue = (entry: StartupEntry) =>
  Boolean(startupDefinitionForEntry(entry)?.value);

export const startupEntryIsRequired = (entry: StartupEntry) =>
  Boolean(startupDefinitionForEntry(entry)?.required);

export const startupEntryAliases = (entry: StartupEntry) =>
  startupDefinitionForEntry(entry)?.aliases || [entry.token];
