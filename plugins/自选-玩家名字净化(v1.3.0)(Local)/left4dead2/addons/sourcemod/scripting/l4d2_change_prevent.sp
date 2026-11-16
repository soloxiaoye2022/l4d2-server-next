#pragma semicolon 1
#pragma newdecls required

#include <sourcemod>
#include <sdktools>

#define PLUGIN_VERSION "1.3.0"
#define PLAYER_NAME_MAX 128

bool g_bNameInitialized[MAXPLAYERS + 1];
bool g_bApplyingName[MAXPLAYERS + 1];
bool g_bCheckScheduled[MAXPLAYERS + 1];
bool g_bHasPendingName[MAXPLAYERS + 1];
char g_sPendingName[MAXPLAYERS + 1][PLAYER_NAME_MAX];

public Plugin myinfo =
{
	name = "l4d2_change_prevent",
	author = "L4D2 Server",
	description = "Clean color markup and invisible characters from player names.",
	version = PLUGIN_VERSION,
	url = ""
};

public void OnClientConnected(int client)
{
	ResetClientState(client);
}

public void OnClientPostAdminCheck(int client)
{
	ScheduleNameCheck(client);
	CreateTimer(0.20, Timer_FinalizeInitialName, GetClientSerial(client), TIMER_FLAG_NO_MAPCHANGE);
}

public void OnClientSettingsChanged(int client)
{
	if (!g_bApplyingName[client])
	{
		ScheduleNameCheck(client);
	}
}

public void OnClientDisconnected(int client)
{
	ResetClientState(client);
}

public Action OnChangeClientName(int client, char[] oldname, char[] newname)
{
	if (client < 1 || client > MaxClients || !IsClientConnected(client) || IsFakeClient(client))
	{
		return Plugin_Continue;
	}

	char cleanName[PLAYER_NAME_MAX];
	SanitizePlayerName(newname, cleanName, sizeof(cleanName));

	if (!StrEqual(newname, cleanName))
	{
		FormatPendingName(cleanName, sizeof(cleanName));
		strcopy(g_sPendingName[client], sizeof(g_sPendingName[]), cleanName);
		g_bHasPendingName[client] = true;
		ScheduleNameCheck(client);
		LogMessage("[NameGuard] Blocked formatted name for client %d: \"%s\" -> \"%s\".", client, newname, cleanName);
		return Plugin_Handled;
	}

	if (g_bApplyingName[client] || !g_bNameInitialized[client])
	{
		return Plugin_Continue;
	}

	// Preserve the previous plugin's behavior: after the initial name is
	// established, ordinary runtime renames are still rejected.
	return Plugin_Handled;
}

public void Frame_CheckName(any serial)
{
	int client = GetClientFromSerial(serial);
	if (client == 0)
	{
		return;
	}

	g_bCheckScheduled[client] = false;
	EnforceCleanName(client);
}

public Action Timer_FinalizeInitialName(Handle timer, any serial)
{
	int client = GetClientFromSerial(serial);
	if (client == 0)
	{
		return Plugin_Stop;
	}

	EnforceCleanName(client);
	g_bNameInitialized[client] = true;
	return Plugin_Stop;
}

void ScheduleNameCheck(int client)
{
	if (client < 1 || client > MaxClients || !IsClientConnected(client) || g_bCheckScheduled[client])
	{
		return;
	}

	g_bCheckScheduled[client] = true;
	RequestFrame(Frame_CheckName, GetClientSerial(client));
}

void EnforceCleanName(int client)
{
	if (client < 1 || client > MaxClients || !IsClientConnected(client) || IsFakeClient(client))
	{
		return;
	}

	char cleanName[PLAYER_NAME_MAX];
	if (g_bHasPendingName[client])
	{
		strcopy(cleanName, sizeof(cleanName), g_sPendingName[client]);
	}
	else
	{
		char currentName[PLAYER_NAME_MAX];
		GetClientName(client, currentName, sizeof(currentName));
		SanitizePlayerName(currentName, cleanName, sizeof(cleanName));
	}

	FormatPendingName(cleanName, sizeof(cleanName));

	char currentName[PLAYER_NAME_MAX];
	GetClientName(client, currentName, sizeof(currentName));
	if (StrEqual(currentName, cleanName))
	{
		g_bHasPendingName[client] = false;
		g_sPendingName[client][0] = '\0';
		return;
	}

	g_bApplyingName[client] = true;
	SetClientName(client, cleanName);
	g_bApplyingName[client] = false;
	g_bHasPendingName[client] = false;
	g_sPendingName[client][0] = '\0';

	LogMessage("[NameGuard] Normalized client %d name: \"%s\" -> \"%s\".", client, currentName, cleanName);
}

void FormatPendingName(char[] name, int maxlength)
{
	if (name[0] == '\0')
	{
		strcopy(name, maxlength, "Player");
	}
}

void ResetClientState(int client)
{
	g_bNameInitialized[client] = false;
	g_bApplyingName[client] = false;
	g_bCheckScheduled[client] = false;
	g_bHasPendingName[client] = false;
	g_sPendingName[client][0] = '\0';
}

void SanitizePlayerName(const char[] input, char[] output, int maxlength)
{
	int outputIndex = 0;

	for (int inputIndex = 0; input[inputIndex] != '\0' && outputIndex < maxlength - 1;)
	{
		int colorTagLength = GetColorTagLength(input, inputIndex);
		if (colorTagLength > 0)
		{
			inputIndex += colorTagLength;
			continue;
		}

		int hiddenLength = GetHiddenSequenceLength(input, inputIndex);
		if (hiddenLength > 0)
		{
			inputIndex += hiddenLength;
			continue;
		}

		int value = view_as<int>(input[inputIndex]) & 0xFF;
		if (value < 0x20 || value == 0x7F)
		{
			inputIndex++;
			continue;
		}

		output[outputIndex++] = input[inputIndex++];
	}

	output[outputIndex] = '\0';
	TrimString(output);
}

int GetColorTagLength(const char[] input, int start)
{
	if (input[start] != '<' || input[start + 1] != '#')
	{
		return 0;
	}

	int index = start + 2;
	int digits = 0;
	while (IsHexDigit(input[index]))
	{
		digits++;
		index++;
	}

	if (input[index] != '>' || (digits != 3 && digits != 4 && digits != 6 && digits != 8))
	{
		return 0;
	}

	return index - start + 1;
}

int GetHiddenSequenceLength(const char[] input, int start)
{
	int b0 = view_as<int>(input[start]) & 0xFF;
	int b1 = view_as<int>(input[start + 1]) & 0xFF;
	int b2 = view_as<int>(input[start + 2]) & 0xFF;

	if (b0 == 0xC2 && b1 == 0xAD)
	{
		return 2;
	}
	if (b0 == 0xCD && b1 == 0x8F)
	{
		return 2;
	}
	if (b0 == 0xE1 && b1 == 0xA0 && b2 == 0x8E)
	{
		return 3;
	}
	if (b0 == 0xE2 && b1 == 0x80)
	{
		if ((b2 >= 0x8B && b2 <= 0x8F) || (b2 >= 0xAA && b2 <= 0xAE))
		{
			return 3;
		}
	}
	if (b0 == 0xE2 && b1 == 0x81)
	{
		if ((b2 >= 0xA0 && b2 <= 0xA4) || (b2 >= 0xA6 && b2 <= 0xA9))
		{
			return 3;
		}
	}
	if (b0 == 0xE2 && b1 == 0xA0 && b2 == 0x80)
	{
		return 3;
	}
	if (b0 == 0xE3 && b1 == 0x85 && b2 == 0xA4)
	{
		return 3;
	}
	if (b0 == 0xEF && b1 == 0xBB && b2 == 0xBF)
	{
		return 3;
	}
	if (b0 == 0xEF && b1 == 0xBE && b2 == 0xA0)
	{
		return 3;
	}

	return 0;
}

bool IsHexDigit(int value)
{
	return (value >= '0' && value <= '9')
		|| (value >= 'a' && value <= 'f')
		|| (value >= 'A' && value <= 'F');
}
