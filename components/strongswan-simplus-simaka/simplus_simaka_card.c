/* SPDX-License-Identifier: GPL-2.0-or-later */
#define _GNU_SOURCE

#include "simplus_simaka_card.h"

#include <pthread.h>
#include <stdlib.h>
#include <string.h>

#include <daemon.h>

typedef struct private_simplus_simaka_card_t private_simplus_simaka_card_t;

struct private_simplus_simaka_card_t {
	simplus_simaka_card_t public;
	simplus_simaka_target_t target;
	char *socket_path;
	char *agent_instance_id;
	char *snapshot_revision;
	char *device_id;
	char *identity_fingerprint;
	char *expected_identity;
	pthread_mutex_t lock;
	bool has_auts;
	uint8_t auts_rand[AKA_RAND_LEN];
	uint8_t auts[AKA_AUTS_LEN];
};

static void clear_bytes(void *value, size_t length)
{
	volatile uint8_t *bytes = value;
	while (length--)
	{
		*bytes++ = 0;
	}
}

static bool identity_matches(private_simplus_simaka_card_t *this, identification_t *id)
{
	chunk_t encoded;
	size_t expected;
	if (!id || !this->expected_identity)
	{
		return false;
	}
	encoded = id->get_encoding(id);
	expected = strlen(this->expected_identity);
	return encoded.len == expected && memeq_const(encoded.ptr, this->expected_identity, expected);
}

METHOD(simaka_card_t, get_triplet, bool,
	private_simplus_simaka_card_t *this, identification_t *id,
	char rand[SIM_RAND_LEN], char sres[SIM_SRES_LEN], char kc[SIM_KC_LEN])
{
	(void)this;
	(void)id;
	(void)rand;
	(void)sres;
	(void)kc;
	return false;
}

METHOD(simaka_card_t, get_quintuplet, status_t,
	private_simplus_simaka_card_t *this, identification_t *id,
	char rand[AKA_RAND_LEN], char autn[AKA_AUTN_LEN], char ck[AKA_CK_LEN],
	char ik[AKA_IK_LEN], char res[AKA_RES_MAX], int *res_len)
{
	simplus_simaka_result_t result;
	simplus_simaka_exchange_stage_t exchange_stage = SIMPLUS_SIMAKA_EXCHANGE_NOT_STARTED;
	status_t status = FAILED;

	memset(&result, 0, sizeof(result));
	memset(ck, 0, AKA_CK_LEN);
	memset(ik, 0, AKA_IK_LEN);
	memset(res, 0, AKA_RES_MAX);
	if (res_len)
	{
		*res_len = 0;
	}
	if (!res_len)
	{
		DBG1(DBG_LIB, "simplus SIM AKA result buffer is unavailable");
		goto done;
	}
	if (!identity_matches(this, id))
	{
		DBG1(DBG_LIB, "simplus SIM AKA identity fence did not match");
		goto done;
	}
	if (!simplus_simaka_agent_authenticate(&this->target,
		(const uint8_t*)rand, (const uint8_t*)autn, &result, &exchange_stage))
	{
		DBG1(DBG_LIB, "simplus SIM AKA Agent exchange failed at stage %s",
			simplus_simaka_exchange_stage_name(exchange_stage));
		goto done;
	}
	DBG1(DBG_LIB, "simplus SIM AKA Agent exchange completed with state %d",
		(int)result.state);
	if (result.state == SIMPLUS_SIMAKA_RESULT_SUCCESS &&
		result.res_len >= 4 && result.res_len <= AKA_RES_MAX)
	{
		memcpy(ck, result.ck, AKA_CK_LEN);
		memcpy(ik, result.ik, AKA_IK_LEN);
		memcpy(res, result.res, result.res_len);
		*res_len = (int)result.res_len;
		status = SUCCESS;
	}
	else if (result.state == SIMPLUS_SIMAKA_RESULT_SYNCHRONIZATION_FAILURE)
	{
		pthread_mutex_lock(&this->lock);
		memcpy(this->auts_rand, rand, AKA_RAND_LEN);
		memcpy(this->auts, result.auts, AKA_AUTS_LEN);
		this->has_auts = true;
		pthread_mutex_unlock(&this->lock);
		status = INVALID_STATE;
	}

done:
	simplus_simaka_result_clear(&result);
	return status;
}

METHOD(simaka_card_t, resync, bool,
	private_simplus_simaka_card_t *this, identification_t *id,
	char rand[AKA_RAND_LEN], char auts[AKA_AUTS_LEN])
{
	bool found = false;
	memset(auts, 0, AKA_AUTS_LEN);
	if (!identity_matches(this, id))
	{
		return false;
	}
	pthread_mutex_lock(&this->lock);
	if (this->has_auts && memeq_const(this->auts_rand, rand, AKA_RAND_LEN))
	{
		memcpy(auts, this->auts, AKA_AUTS_LEN);
		found = true;
	}
	clear_bytes(this->auts_rand, sizeof(this->auts_rand));
	clear_bytes(this->auts, sizeof(this->auts));
	this->has_auts = false;
	pthread_mutex_unlock(&this->lock);
	return found;
}

METHOD(simplus_simaka_card_t, destroy, void,
	private_simplus_simaka_card_t *this)
{
	pthread_mutex_destroy(&this->lock);
	clear_bytes(this->auts_rand, sizeof(this->auts_rand));
	clear_bytes(this->auts, sizeof(this->auts));
	if (this->expected_identity)
	{
		clear_bytes(this->expected_identity, strlen(this->expected_identity));
	}
	free(this->socket_path);
	free(this->agent_instance_id);
	free(this->snapshot_revision);
	free(this->device_id);
	free(this->identity_fingerprint);
	free(this->expected_identity);
	free(this);
}

simplus_simaka_card_t *simplus_simaka_card_create(
	const simplus_simaka_target_t *target,
	const char *expected_identity)
{
	private_simplus_simaka_card_t *this;
	if (!target || !target->socket_path || !target->agent_instance_id ||
		!target->snapshot_revision || !target->device_id ||
		!target->identity_fingerprint || !expected_identity || !expected_identity[0])
	{
		return NULL;
	}
	INIT(this,
		.public = {
			.card = {
				.get_triplet = _get_triplet,
				.get_quintuplet = _get_quintuplet,
				.resync = _resync,
				.get_pseudonym = (void*)return_null,
				.set_pseudonym = (void*)nop,
				.get_reauth = (void*)return_null,
				.set_reauth = (void*)nop,
			},
			.destroy = _destroy,
		},
		.socket_path = strdup(target->socket_path),
		.agent_instance_id = strdup(target->agent_instance_id),
		.snapshot_revision = strdup(target->snapshot_revision),
		.device_id = strdup(target->device_id),
		.identity_fingerprint = strdup(target->identity_fingerprint),
		.expected_identity = strdup(expected_identity),
	);
	if (!this || !this->socket_path || !this->agent_instance_id ||
		!this->snapshot_revision || !this->device_id ||
		!this->identity_fingerprint || !this->expected_identity ||
		pthread_mutex_init(&this->lock, NULL) != 0)
	{
		if (this)
		{
			free(this->socket_path);
			free(this->agent_instance_id);
			free(this->snapshot_revision);
			free(this->device_id);
			free(this->identity_fingerprint);
			free(this->expected_identity);
			free(this);
		}
		return NULL;
	}
	this->target = (simplus_simaka_target_t){
		.socket_path = this->socket_path,
		.agent_instance_id = this->agent_instance_id,
		.snapshot_generation = target->snapshot_generation,
		.snapshot_revision = this->snapshot_revision,
		.device_id = this->device_id,
		.device_generation = target->device_generation,
		.identity_fingerprint = this->identity_fingerprint,
	};
	return &this->public;
}
