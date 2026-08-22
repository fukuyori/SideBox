#ifndef SIDEBOX_DARWIN_H
#define SIDEBOX_DARWIN_H

void SideboxRun(const char *config_json, const char *config_path, const char *version);

extern char *SideboxFetchWeatherJSON(void);
extern char *SideboxReloadConfigJSON(void);
extern void SideboxSaveWindowFrame(int x, int y, int width, int height);
extern char *SideboxSetStartAtLogin(int enabled);

#endif
