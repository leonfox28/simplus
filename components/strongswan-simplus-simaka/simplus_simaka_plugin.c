/* SPDX-License-Identifier: GPL-2.0-or-later */
#define _GNU_SOURCE

#include "simplus_simaka_plugin.h"
#include "simplus_simaka_apn.h"
#include "simplus_simaka_card.h"

#include <errno.h>
#include <stdlib.h>

#include <daemon.h>
#include <library.h>

typedef struct private_simplus_simaka_plugin_t private_simplus_simaka_plugin_t;

struct private_simplus_simaka_plugin_t {
	simplus_simaka_plugin_t public;
	simplus_simaka_card_t *card;
	simplus_simaka_apn_t *apn;
};

METHOD(plugin_t, get_name, char*, private_simplus_simaka_plugin_t *this)
{
	(void)this;
	return "simplus-simaka";
}

static simaka_card_t *get_card(private_simplus_simaka_plugin_t *this)
{
	return &this->card->card;
}

static bool apn_listener_cb(private_simplus_simaka_plugin_t *this,
	plugin_feature_t *feature, bool reg, void *cb_data)
{
	(void)feature;
	(void)cb_data;
	if (reg)
	{
		charon->bus->add_listener(charon->bus, &this->apn->listener);
	}
	else
	{
		charon->bus->remove_listener(charon->bus, &this->apn->listener);
	}
	return TRUE;
}

METHOD(plugin_t, get_features, int,
	private_simplus_simaka_plugin_t *this, plugin_feature_t *features[])
{
	(void)this;
	static plugin_feature_t f[] = {
		PLUGIN_CALLBACK(simaka_manager_register, get_card),
			PLUGIN_PROVIDE(CUSTOM, "aka-card"),
				PLUGIN_DEPENDS(CUSTOM, "aka-manager"),
		PLUGIN_CALLBACK((plugin_feature_callback_t)apn_listener_cb, NULL),
			PLUGIN_PROVIDE(CUSTOM, "simplus-ims-apn-idr"),
	};
	*features = f;
	return countof(f);
}

METHOD(plugin_t, destroy, void, private_simplus_simaka_plugin_t *this)
{
	this->card->destroy(this->card);
	this->apn->destroy(this->apn);
	free(this);
}

static bool parse_u64(const char *value, uint64_t *result)
{
	char *end;
	unsigned long long parsed;
	if (!value || !value[0])
	{
		return false;
	}
	errno = 0;
	parsed = strtoull(value, &end, 10);
	if (errno || !end || *end || parsed == 0)
	{
		return false;
	}
	*result = (uint64_t)parsed;
	return true;
}

plugin_t *simplus_simaka_plugin_create(void)
{
	private_simplus_simaka_plugin_t *this;
	simplus_simaka_target_t target = {0};
	const char *snapshot_generation, *device_generation, *expected_identity;

	/*
	 * charon deliberately drops all capabilities that no loaded plugin asks
	 * it to retain.  This plugin connects to a mode 0600 socket owned by the
	 * unprivileged Agent service, so retain the same narrow DAC capability as
	 * strongSwan's upstream ssh-agent plugin.  The HIL daemon remains an
	 * ephemeral root process isolated in its own network namespace.
	 */
	if (!lib->caps->keep(lib->caps, CAP_DAC_OVERRIDE))
	{
		DBG1(DBG_DMN, "simplus SIM AKA plugin requires CAP_DAC_OVERRIDE capability");
		return NULL;
	}

	target.socket_path = lib->settings->get_str(lib->settings,
		"%s.plugins.simplus-simaka.socket", NULL, lib->ns);
	target.agent_instance_id = lib->settings->get_str(lib->settings,
		"%s.plugins.simplus-simaka.agent_instance_id", NULL, lib->ns);
	snapshot_generation = lib->settings->get_str(lib->settings,
		"%s.plugins.simplus-simaka.snapshot_generation", NULL, lib->ns);
	target.snapshot_revision = lib->settings->get_str(lib->settings,
		"%s.plugins.simplus-simaka.snapshot_revision", NULL, lib->ns);
	target.device_id = lib->settings->get_str(lib->settings,
		"%s.plugins.simplus-simaka.device_id", NULL, lib->ns);
	device_generation = lib->settings->get_str(lib->settings,
		"%s.plugins.simplus-simaka.device_generation", NULL, lib->ns);
	target.identity_fingerprint = lib->settings->get_str(lib->settings,
		"%s.plugins.simplus-simaka.identity_fingerprint", NULL, lib->ns);
	expected_identity = lib->settings->get_str(lib->settings,
		"%s.plugins.simplus-simaka.expected_identity", NULL, lib->ns);
	if (!parse_u64(snapshot_generation, &target.snapshot_generation) ||
		!parse_u64(device_generation, &target.device_generation))
	{
		return NULL;
	}

	INIT(this,
		.public = {
			.plugin = {
				.get_name = _get_name,
				.get_features = _get_features,
				.destroy = _destroy,
			},
		},
		.card = simplus_simaka_card_create(&target, expected_identity),
		.apn = simplus_simaka_apn_create(),
	);
	if (!this || !this->card || !this->apn)
	{
		if (this)
		{
			DESTROY_IF(this->card);
			DESTROY_IF(this->apn);
		}
		free(this);
		return NULL;
	}
	return &this->public.plugin;
}
