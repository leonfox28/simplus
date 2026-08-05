/* SPDX-License-Identifier: GPL-2.0-or-later */
#include "simplus_simaka_agent.h"

#include <assert.h>
#include <stdio.h>
#include <string.h>

static simplus_simaka_target_t target = {
	.socket_path = "/run/simplus-agent/sim-aka.sock",
	.agent_instance_id = "01234567-89ab-cdef-0123-456789abcdef",
	.snapshot_generation = 1,
	.snapshot_revision = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	.device_id = "usb-1-3",
	.device_generation = 2,
	.identity_fingerprint = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
};

static void test_success(void)
{
	char response[] =
		"HTTP/1.1 200 OK\r\nContent-Type: application/json\r\n\r\n"
		"{\"protocolVersion\":1,\"agentInstanceId\":\"01234567-89ab-cdef-0123-456789abcdef\","
		"\"deviceId\":\"usb-1-3\",\"exchangeId\":\"00112233445566778899aabbccddeeff\","
		"\"result\":{\"state\":\"success\",\"res\":\"0102030405060708\","
		"\"ck\":\"000102030405060708090a0b0c0d0e0f\","
		"\"ik\":\"101112131415161718191a1b1c1d1e1f\"}}";
	simplus_simaka_result_t result;
	assert(simplus_simaka_parse_response_for_test(&target,
		"00112233445566778899aabbccddeeff", response, &result));
	assert(result.state == SIMPLUS_SIMAKA_RESULT_SUCCESS);
	assert(result.res_len == 8 && result.res[0] == 1 && result.res[7] == 8);
	assert(result.ck[15] == 0x0f && result.ik[0] == 0x10);
	simplus_simaka_result_clear(&result);
}

static void test_sync_failure(void)
{
	char response[] =
		"HTTP/1.1 200 OK\r\n\r\n"
		"{\"protocolVersion\":1,\"agentInstanceId\":\"01234567-89ab-cdef-0123-456789abcdef\","
		"\"deviceId\":\"usb-1-3\",\"exchangeId\":\"00112233445566778899aabbccddeeff\","
		"\"result\":{\"state\":\"synchronization-failure\","
		"\"auts\":\"000102030405060708090a0b0c0d\"}}";
	simplus_simaka_result_t result;
	assert(simplus_simaka_parse_response_for_test(&target,
		"00112233445566778899aabbccddeeff", response, &result));
	assert(result.state == SIMPLUS_SIMAKA_RESULT_SYNCHRONIZATION_FAILURE);
	assert(result.auts[13] == 0x0d);
	simplus_simaka_result_clear(&result);
}

static void test_rejects_mismatch_and_noncanonical_hex(void)
{
	char mismatch[] =
		"HTTP/1.1 200 OK\r\n\r\n"
		"{\"agentInstanceId\":\"fedcba98-7654-3210-fedc-ba9876543210\","
		"\"deviceId\":\"usb-1-3\",\"exchangeId\":\"00112233445566778899aabbccddeeff\","
		"\"result\":{\"state\":\"success\",\"res\":\"01020304\","
		"\"ck\":\"000102030405060708090A0B0C0D0E0F\","
		"\"ik\":\"101112131415161718191a1b1c1d1e1f\"}}";
	simplus_simaka_result_t result;
	assert(!simplus_simaka_parse_response_for_test(&target,
		"00112233445566778899aabbccddeeff", mismatch, &result));
	assert(result.state == SIMPLUS_SIMAKA_RESULT_FAILED);
}

int main(void)
{
	assert(strcmp(simplus_simaka_exchange_stage_name(
		SIMPLUS_SIMAKA_EXCHANGE_CONNECT), "connect") == 0);
	assert(strcmp(simplus_simaka_exchange_stage_name(
		(simplus_simaka_exchange_stage_t)99), "not-started") == 0);
	test_success();
	test_sync_failure();
	test_rejects_mismatch_and_noncanonical_hex();
	puts("simplus_simaka_agent tests passed");
	return 0;
}
