/* SPDX-License-Identifier: GPL-2.0-or-later */
#ifndef SIMPLUS_SIMAKA_AGENT_H_
#define SIMPLUS_SIMAKA_AGENT_H_

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

#define SIMPLUS_AKA_RAND_LEN 16
#define SIMPLUS_AKA_AUTN_LEN 16
#define SIMPLUS_AKA_RES_MAX 16
#define SIMPLUS_AKA_CK_LEN 16
#define SIMPLUS_AKA_IK_LEN 16
#define SIMPLUS_AKA_AUTS_LEN 14

typedef struct {
	const char *socket_path;
	const char *agent_instance_id;
	uint64_t snapshot_generation;
	const char *snapshot_revision;
	const char *device_id;
	uint64_t device_generation;
	const char *identity_fingerprint;
} simplus_simaka_target_t;

typedef enum {
	SIMPLUS_SIMAKA_RESULT_FAILED = 0,
	SIMPLUS_SIMAKA_RESULT_SUCCESS,
	SIMPLUS_SIMAKA_RESULT_SYNCHRONIZATION_FAILURE,
} simplus_simaka_result_state_t;

typedef struct {
	simplus_simaka_result_state_t state;
	uint8_t res[SIMPLUS_AKA_RES_MAX];
	size_t res_len;
	uint8_t ck[SIMPLUS_AKA_CK_LEN];
	uint8_t ik[SIMPLUS_AKA_IK_LEN];
	uint8_t auts[SIMPLUS_AKA_AUTS_LEN];
} simplus_simaka_result_t;

typedef enum {
	SIMPLUS_SIMAKA_EXCHANGE_NOT_STARTED = 0,
	SIMPLUS_SIMAKA_EXCHANGE_INVALID_INPUT,
	SIMPLUS_SIMAKA_EXCHANGE_RANDOM_ID,
	SIMPLUS_SIMAKA_EXCHANGE_REQUEST_ENCODING,
	SIMPLUS_SIMAKA_EXCHANGE_SOCKET_OPEN,
	SIMPLUS_SIMAKA_EXCHANGE_SOCKET_OPTIONS,
	SIMPLUS_SIMAKA_EXCHANGE_CONNECT,
	SIMPLUS_SIMAKA_EXCHANGE_WRITE,
	SIMPLUS_SIMAKA_EXCHANGE_READ,
	SIMPLUS_SIMAKA_EXCHANGE_RESPONSE_SIZE,
	SIMPLUS_SIMAKA_EXCHANGE_RESPONSE_PARSE,
	SIMPLUS_SIMAKA_EXCHANGE_COMPLETE,
} simplus_simaka_exchange_stage_t;

bool simplus_simaka_agent_authenticate(
	const simplus_simaka_target_t *target,
	const uint8_t rand[SIMPLUS_AKA_RAND_LEN],
	const uint8_t autn[SIMPLUS_AKA_AUTN_LEN],
	simplus_simaka_result_t *result,
	simplus_simaka_exchange_stage_t *stage);

const char *simplus_simaka_exchange_stage_name(
	simplus_simaka_exchange_stage_t stage);

void simplus_simaka_result_clear(simplus_simaka_result_t *result);

#ifdef SIMPLUS_SIMAKA_TEST
bool simplus_simaka_parse_response_for_test(
	const simplus_simaka_target_t *target,
	const char *exchange_id,
	char *response,
	simplus_simaka_result_t *result);
#endif

#endif /* SIMPLUS_SIMAKA_AGENT_H_ */
