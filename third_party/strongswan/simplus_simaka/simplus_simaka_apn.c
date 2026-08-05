/* SPDX-License-Identifier: GPL-2.0-or-later */
#include "simplus_simaka_apn.h"

#include <daemon.h>
#include <encoding/message.h>
#include <encoding/payloads/id_payload.h>
#include <sa/ike_sa.h>

#define SIMPLUS_VOWIFI_CONNECTION "vowifi-ims"
#define SIMPLUS_VOWIFI_APN_ID "fqdn:ims"

typedef struct private_simplus_simaka_apn_t private_simplus_simaka_apn_t;

struct private_simplus_simaka_apn_t {
	simplus_simaka_apn_t public;
};

METHOD(listener_t, message, bool,
	private_simplus_simaka_apn_t *this, ike_sa_t *ike_sa,
	message_t *message, bool incoming, bool plain)
{
	identification_t *apn;
	id_payload_t *payload;

	(void)this;
	if (incoming || !plain || !ike_sa ||
		message->get_exchange_type(message) != IKE_AUTH ||
		!message->get_request(message) || message->get_message_id(message) != 1 ||
		!streq(ike_sa->get_name(ike_sa), SIMPLUS_VOWIFI_CONNECTION))
	{
		return TRUE;
	}
	if (message->get_payload(message, PLV2_ID_RESPONDER))
	{
		DBG1(DBG_IKE, "simplus IMS APN IDr already present, refusing replacement");
		return TRUE;
	}
	apn = identification_create_from_string(SIMPLUS_VOWIFI_APN_ID);
	if (!apn || apn->get_type(apn) != ID_FQDN)
	{
		DESTROY_IF(apn);
		DBG1(DBG_IKE, "simplus IMS APN IDr construction failed");
		return TRUE;
	}
	payload = id_payload_create_from_identification(PLV2_ID_RESPONDER, apn);
	apn->destroy(apn);
	if (!payload)
	{
		DBG1(DBG_IKE, "simplus IMS APN IDr construction failed");
		return TRUE;
	}
	message->add_payload(message, (payload_t*)payload);
	DBG1(DBG_IKE, "simplus IMS APN IDr added");
	return TRUE;
}

METHOD(simplus_simaka_apn_t, destroy, void,
	private_simplus_simaka_apn_t *this)
{
	free(this);
}

simplus_simaka_apn_t *simplus_simaka_apn_create(void)
{
	private_simplus_simaka_apn_t *this;

	INIT(this,
		.public = {
			.listener = {
				.message = _message,
			},
			.destroy = _destroy,
		},
	);
	return &this->public;
}
