/* SPDX-License-Identifier: GPL-2.0-or-later */
#define _GNU_SOURCE

#include "simplus_simaka_agent.h"

#include <errno.h>
#include <fcntl.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/random.h>
#include <sys/socket.h>
#include <sys/time.h>
#include <sys/un.h>
#include <unistd.h>

#define HTTP_BUFFER_SIZE 8192
#define REQUEST_BUFFER_SIZE 4096
#define EXCHANGE_ID_BYTES 16
#define EXCHANGE_ID_CHARS (EXCHANGE_ID_BYTES * 2)

static void secure_clear(void *value, size_t length)
{
	volatile uint8_t *bytes = value;
	while (length--)
	{
		*bytes++ = 0;
	}
}

void simplus_simaka_result_clear(simplus_simaka_result_t *result)
{
	if (result)
	{
		secure_clear(result, sizeof(*result));
	}
}

const char *simplus_simaka_exchange_stage_name(
	simplus_simaka_exchange_stage_t stage)
{
	switch (stage)
	{
		case SIMPLUS_SIMAKA_EXCHANGE_INVALID_INPUT:
			return "invalid-input";
		case SIMPLUS_SIMAKA_EXCHANGE_RANDOM_ID:
			return "random-id";
		case SIMPLUS_SIMAKA_EXCHANGE_REQUEST_ENCODING:
			return "request-encoding";
		case SIMPLUS_SIMAKA_EXCHANGE_SOCKET_OPEN:
			return "socket-open";
		case SIMPLUS_SIMAKA_EXCHANGE_SOCKET_OPTIONS:
			return "socket-options";
		case SIMPLUS_SIMAKA_EXCHANGE_CONNECT:
			return "connect";
		case SIMPLUS_SIMAKA_EXCHANGE_WRITE:
			return "write";
		case SIMPLUS_SIMAKA_EXCHANGE_READ:
			return "read";
		case SIMPLUS_SIMAKA_EXCHANGE_RESPONSE_SIZE:
			return "response-size";
		case SIMPLUS_SIMAKA_EXCHANGE_RESPONSE_PARSE:
			return "response-parse";
		case SIMPLUS_SIMAKA_EXCHANGE_COMPLETE:
			return "complete";
		case SIMPLUS_SIMAKA_EXCHANGE_NOT_STARTED:
		default:
			return "not-started";
	}
}

static bool valid_token(const char *value, size_t minimum, size_t maximum)
{
	size_t index, length;

	if (!value)
	{
		return false;
	}
	length = strnlen(value, maximum + 1);
	if (length < minimum || length > maximum)
	{
		return false;
	}
	for (index = 0; index < length; index++)
	{
		char byte = value[index];
		if (!((byte >= 'a' && byte <= 'z') || (byte >= 'A' && byte <= 'Z') ||
			  (byte >= '0' && byte <= '9') || byte == '-' || byte == '_' || byte == '.'))
		{
			return false;
		}
	}
	return true;
}

static bool valid_lower_hex(const char *value, size_t length)
{
	size_t index;
	if (!value || strlen(value) != length)
	{
		return false;
	}
	for (index = 0; index < length; index++)
	{
		if (!((value[index] >= '0' && value[index] <= '9') ||
			  (value[index] >= 'a' && value[index] <= 'f')))
		{
			return false;
		}
	}
	return true;
}

static bool valid_target(const simplus_simaka_target_t *target)
{
	if (!target || !target->socket_path || target->socket_path[0] != '/' ||
		strnlen(target->socket_path, sizeof(((struct sockaddr_un*)0)->sun_path)) >= sizeof(((struct sockaddr_un*)0)->sun_path))
	{
		return false;
	}
	return valid_token(target->agent_instance_id, 36, 36) &&
		target->snapshot_generation != 0 &&
		valid_lower_hex(target->snapshot_revision, 64) &&
		valid_token(target->device_id, 1, 128) &&
		target->device_generation != 0 &&
		valid_lower_hex(target->identity_fingerprint, 64);
}

static void encode_hex(const uint8_t *input, size_t length, char *output)
{
	static const char alphabet[] = "0123456789abcdef";
	size_t index;
	for (index = 0; index < length; index++)
	{
		output[index * 2] = alphabet[input[index] >> 4];
		output[index * 2 + 1] = alphabet[input[index] & 0x0f];
	}
	output[length * 2] = '\0';
}

static int hex_nibble(char value)
{
	if (value >= '0' && value <= '9')
	{
		return value - '0';
	}
	if (value >= 'a' && value <= 'f')
	{
		return value - 'a' + 10;
	}
	return -1;
}

static bool decode_hex(const char *input, size_t bytes, uint8_t *output)
{
	size_t index;
	if (!input || strlen(input) != bytes * 2)
	{
		return false;
	}
	for (index = 0; index < bytes; index++)
	{
		int high = hex_nibble(input[index * 2]);
		int low = hex_nibble(input[index * 2 + 1]);
		if (high < 0 || low < 0)
		{
			return false;
		}
		output[index] = (uint8_t)((high << 4) | low);
	}
	return true;
}

static bool random_exchange_id(char output[EXCHANGE_ID_CHARS + 1])
{
	uint8_t random[EXCHANGE_ID_BYTES];
	size_t offset = 0;
	while (offset < sizeof(random))
	{
		ssize_t count = getrandom(random + offset, sizeof(random) - offset, 0);
		if (count < 0 && errno == EINTR)
		{
			continue;
		}
		if (count <= 0)
		{
			secure_clear(random, sizeof(random));
			return false;
		}
		offset += (size_t)count;
	}
	encode_hex(random, sizeof(random), output);
	secure_clear(random, sizeof(random));
	return true;
}

static bool write_all(int descriptor, const char *data, size_t length)
{
	size_t offset = 0;
	while (offset < length)
	{
		ssize_t count = send(descriptor, data + offset, length - offset, MSG_NOSIGNAL);
		if (count < 0 && errno == EINTR)
		{
			continue;
		}
		if (count <= 0)
		{
			return false;
		}
		offset += (size_t)count;
	}
	return true;
}

static bool json_string(const char *body, const char *key, char *output, size_t output_size)
{
	char pattern[96];
	const char *start, *end;
	size_t length, index;

	if (snprintf(pattern, sizeof(pattern), "\"%s\":\"", key) < 0)
	{
		return false;
	}
	start = strstr(body, pattern);
	if (!start)
	{
		return false;
	}
	start += strlen(pattern);
	end = strchr(start, '"');
	if (!end)
	{
		return false;
	}
	length = (size_t)(end - start);
	if (length == 0 || length >= output_size)
	{
		return false;
	}
	for (index = 0; index < length; index++)
	{
		unsigned char byte = (unsigned char)start[index];
		if (byte < 0x20 || byte == '\\')
		{
			return false;
		}
	}
	memcpy(output, start, length);
	output[length] = '\0';
	return true;
}

static bool parse_response(
	const simplus_simaka_target_t *target,
	const char *exchange_id,
	char *response,
	simplus_simaka_result_t *result)
{
	char *body;
	char agent[64], device[129], exchange[EXCHANGE_ID_CHARS + 1], state[64];
	char res[SIMPLUS_AKA_RES_MAX * 2 + 1], ck[SIMPLUS_AKA_CK_LEN * 2 + 1];
	char ik[SIMPLUS_AKA_IK_LEN * 2 + 1], auts[SIMPLUS_AKA_AUTS_LEN * 2 + 1];
	bool success = false;

	memset(agent, 0, sizeof(agent));
	memset(device, 0, sizeof(device));
	memset(exchange, 0, sizeof(exchange));
	memset(state, 0, sizeof(state));
	memset(res, 0, sizeof(res));
	memset(ck, 0, sizeof(ck));
	memset(ik, 0, sizeof(ik));
	memset(auts, 0, sizeof(auts));
	simplus_simaka_result_clear(result);

	if (!(strncmp(response, "HTTP/1.1 200 ", 13) == 0 ||
		  strncmp(response, "HTTP/1.0 200 ", 13) == 0))
	{
		goto done;
	}
	body = strstr(response, "\r\n\r\n");
	if (!body)
	{
		goto done;
	}
	body += 4;
	if (!strstr(body, "\"protocolVersion\":1") ||
		!json_string(body, "agentInstanceId", agent, sizeof(agent)) ||
		!json_string(body, "deviceId", device, sizeof(device)) ||
		!json_string(body, "exchangeId", exchange, sizeof(exchange)) ||
		!json_string(body, "state", state, sizeof(state)) ||
		strcmp(agent, target->agent_instance_id) != 0 ||
		strcmp(device, target->device_id) != 0 ||
		strcmp(exchange, exchange_id) != 0)
	{
		goto done;
	}
	if (strcmp(state, "success") == 0)
	{
		size_t res_bytes;
		if (!json_string(body, "res", res, sizeof(res)) || strlen(res) < 8 ||
			strlen(res) > SIMPLUS_AKA_RES_MAX * 2 || strlen(res) % 2 != 0 ||
			!json_string(body, "ck", ck, sizeof(ck)) ||
			!json_string(body, "ik", ik, sizeof(ik)))
		{
			goto done;
		}
		res_bytes = strlen(res) / 2;
		if (!decode_hex(res, res_bytes, result->res) ||
			!decode_hex(ck, SIMPLUS_AKA_CK_LEN, result->ck) ||
			!decode_hex(ik, SIMPLUS_AKA_IK_LEN, result->ik))
		{
			goto done;
		}
		result->res_len = res_bytes;
		result->state = SIMPLUS_SIMAKA_RESULT_SUCCESS;
		success = true;
	}
	else if (strcmp(state, "synchronization-failure") == 0)
	{
		if (!json_string(body, "auts", auts, sizeof(auts)) ||
			!decode_hex(auts, SIMPLUS_AKA_AUTS_LEN, result->auts))
		{
			goto done;
		}
		result->state = SIMPLUS_SIMAKA_RESULT_SYNCHRONIZATION_FAILURE;
		success = true;
	}

done:
	if (!success)
	{
		simplus_simaka_result_clear(result);
	}
	secure_clear(agent, sizeof(agent));
	secure_clear(device, sizeof(device));
	secure_clear(exchange, sizeof(exchange));
	secure_clear(state, sizeof(state));
	secure_clear(res, sizeof(res));
	secure_clear(ck, sizeof(ck));
	secure_clear(ik, sizeof(ik));
	secure_clear(auts, sizeof(auts));
	return success;
}

bool simplus_simaka_agent_authenticate(
	const simplus_simaka_target_t *target,
	const uint8_t rand[SIMPLUS_AKA_RAND_LEN],
	const uint8_t autn[SIMPLUS_AKA_AUTN_LEN],
	simplus_simaka_result_t *result,
	simplus_simaka_exchange_stage_t *stage)
{
	struct sockaddr_un address;
	struct timeval timeout = {.tv_sec = 6, .tv_usec = 0};
	char rand_hex[SIMPLUS_AKA_RAND_LEN * 2 + 1];
	char autn_hex[SIMPLUS_AKA_AUTN_LEN * 2 + 1];
	char exchange_id[EXCHANGE_ID_CHARS + 1];
	char body[2048], request[REQUEST_BUFFER_SIZE], response[HTTP_BUFFER_SIZE];
	int descriptor = -1, body_length, request_length;
	size_t response_length = 0;
	bool success = false;

	if (stage)
	{
		*stage = SIMPLUS_SIMAKA_EXCHANGE_NOT_STARTED;
	}
	simplus_simaka_result_clear(result);
	memset(&address, 0, sizeof(address));
	memset(rand_hex, 0, sizeof(rand_hex));
	memset(autn_hex, 0, sizeof(autn_hex));
	memset(exchange_id, 0, sizeof(exchange_id));
	memset(body, 0, sizeof(body));
	memset(request, 0, sizeof(request));
	memset(response, 0, sizeof(response));

	if (!result || !rand || !autn || !valid_target(target))
	{
		if (stage)
		{
			*stage = SIMPLUS_SIMAKA_EXCHANGE_INVALID_INPUT;
		}
		goto done;
	}
	if (!random_exchange_id(exchange_id))
	{
		if (stage)
		{
			*stage = SIMPLUS_SIMAKA_EXCHANGE_RANDOM_ID;
		}
		goto done;
	}
	encode_hex(rand, SIMPLUS_AKA_RAND_LEN, rand_hex);
	encode_hex(autn, SIMPLUS_AKA_AUTN_LEN, autn_hex);
	body_length = snprintf(body, sizeof(body),
		"{\"agentInstanceId\":\"%s\",\"snapshotGeneration\":%llu,"
		"\"snapshotRevision\":\"%s\",\"deviceId\":\"%s\","
		"\"deviceGeneration\":%llu,\"identityFingerprint\":\"%s\","
		"\"exchangeId\":\"%s\",\"rand\":\"%s\",\"autn\":\"%s\"}",
		target->agent_instance_id, (unsigned long long)target->snapshot_generation,
		target->snapshot_revision, target->device_id,
		(unsigned long long)target->device_generation, target->identity_fingerprint,
		exchange_id, rand_hex, autn_hex);
	if (body_length <= 0 || (size_t)body_length >= sizeof(body))
	{
		if (stage)
		{
			*stage = SIMPLUS_SIMAKA_EXCHANGE_REQUEST_ENCODING;
		}
		goto done;
	}
	request_length = snprintf(request, sizeof(request),
		"POST /v1/sim/aka/authenticate HTTP/1.1\r\n"
		"Host: unix\r\nContent-Type: application/json\r\n"
		"Content-Length: %d\r\nConnection: close\r\n\r\n%s",
		body_length, body);
	if (request_length <= 0 || (size_t)request_length >= sizeof(request))
	{
		if (stage)
		{
			*stage = SIMPLUS_SIMAKA_EXCHANGE_REQUEST_ENCODING;
		}
		goto done;
	}

	descriptor = socket(AF_UNIX, SOCK_STREAM | SOCK_CLOEXEC, 0);
	if (descriptor < 0)
	{
		if (stage)
		{
			*stage = SIMPLUS_SIMAKA_EXCHANGE_SOCKET_OPEN;
		}
		goto done;
	}
	if (setsockopt(descriptor, SOL_SOCKET, SO_RCVTIMEO, &timeout, sizeof(timeout)) != 0 ||
		setsockopt(descriptor, SOL_SOCKET, SO_SNDTIMEO, &timeout, sizeof(timeout)) != 0)
	{
		if (stage)
		{
			*stage = SIMPLUS_SIMAKA_EXCHANGE_SOCKET_OPTIONS;
		}
		goto done;
	}
	address.sun_family = AF_UNIX;
	memcpy(address.sun_path, target->socket_path, strlen(target->socket_path) + 1);
	if (connect(descriptor, (struct sockaddr*)&address, sizeof(address)) != 0)
	{
		if (stage)
		{
			*stage = SIMPLUS_SIMAKA_EXCHANGE_CONNECT;
		}
		goto done;
	}
	if (!write_all(descriptor, request, (size_t)request_length))
	{
		if (stage)
		{
			*stage = SIMPLUS_SIMAKA_EXCHANGE_WRITE;
		}
		goto done;
	}
	while (response_length < sizeof(response) - 1)
	{
		ssize_t count = read(descriptor, response + response_length,
							 sizeof(response) - response_length - 1);
		if (count < 0 && errno == EINTR)
		{
			continue;
		}
		if (count < 0)
		{
			if (stage)
			{
				*stage = SIMPLUS_SIMAKA_EXCHANGE_READ;
			}
			goto done;
		}
		if (count == 0)
		{
			break;
		}
		response_length += (size_t)count;
	}
	if (response_length == 0 || response_length >= sizeof(response) - 1)
	{
		if (stage)
		{
			*stage = SIMPLUS_SIMAKA_EXCHANGE_RESPONSE_SIZE;
		}
		goto done;
	}
	response[response_length] = '\0';
	success = parse_response(target, exchange_id, response, result);
	if (stage)
	{
		*stage = success ? SIMPLUS_SIMAKA_EXCHANGE_COMPLETE :
			SIMPLUS_SIMAKA_EXCHANGE_RESPONSE_PARSE;
	}

done:
	if (descriptor >= 0)
	{
		close(descriptor);
	}
	if (!success)
	{
		simplus_simaka_result_clear(result);
	}
	secure_clear(rand_hex, sizeof(rand_hex));
	secure_clear(autn_hex, sizeof(autn_hex));
	secure_clear(exchange_id, sizeof(exchange_id));
	secure_clear(body, sizeof(body));
	secure_clear(request, sizeof(request));
	secure_clear(response, sizeof(response));
	return success;
}

#ifdef SIMPLUS_SIMAKA_TEST
bool simplus_simaka_parse_response_for_test(
	const simplus_simaka_target_t *target,
	const char *exchange_id,
	char *response,
	simplus_simaka_result_t *result)
{
	return parse_response(target, exchange_id, response, result);
}
#endif
