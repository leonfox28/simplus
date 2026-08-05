AM_CPPFLAGS = \
	-I$(top_srcdir)/src/libstrongswan \
	-I$(top_srcdir)/src/libcharon \
	-I$(top_srcdir)/src/libsimaka

AM_CFLAGS = $(PLUGIN_CFLAGS)

libstrongswan_simplus_simaka_la_LDFLAGS = -module -avoid-version

if MONOLITHIC
noinst_LTLIBRARIES = libstrongswan-simplus-simaka.la
else
plugin_LTLIBRARIES = libstrongswan-simplus-simaka.la
libstrongswan_simplus_simaka_la_LIBADD = $(top_builddir)/src/libsimaka/libsimaka.la
endif

libstrongswan_simplus_simaka_la_SOURCES = \
	simplus_simaka_plugin.h simplus_simaka_plugin.c \
	simplus_simaka_apn.h simplus_simaka_apn.c \
	simplus_simaka_card.h simplus_simaka_card.c \
	simplus_simaka_agent.h simplus_simaka_agent.c
