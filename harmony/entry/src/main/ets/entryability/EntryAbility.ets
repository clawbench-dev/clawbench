import { UIAbility, ConfigurationConstant, Want } from '@kit.AbilityKit';
import { window } from '@kit.ArkUI';
import { NativeEventService } from '../services/NativeEventService';
import { ShareImportService } from '../services/ShareImportService';

export default class EntryAbility extends UIAbility {
  onCreate(want: Want): void {
    NativeEventService.receiveWant(want);
    ShareImportService.get(this.context).receive(want).catch(() => {
      AppStorage.setOrCreate('clawbenchShareError', true);
    });
    this.context.getApplicationContext().setColorMode(ConfigurationConstant.ColorMode.COLOR_MODE_DARK);
  }

  onNewWant(want: Want): void {
    NativeEventService.receiveWant(want);
    ShareImportService.get(this.context).receive(want).catch(() => {
      AppStorage.setOrCreate('clawbenchShareError', true);
    });
  }

  onWindowStageCreate(stage: window.WindowStage): void {
    this.loadWindow(stage);
  }

  private async loadWindow(stage: window.WindowStage): Promise<void> {
    const mainWindow = stage.getMainWindowSync();
    try {
      await mainWindow.setWindowLayoutFullScreen(true);
      await mainWindow.setWindowSystemBarProperties({
        statusBarColor: '#17191D', navigationBarColor: '#17191D',
        statusBarContentColor: '#FFFFFF', navigationBarContentColor: '#FFFFFF'
      });
    } catch (_) {
      // Keep the normal safe layout if immersive configuration is unavailable.
    }
    stage.loadContent('pages/Index');
  }

  onForeground(): void {
    AppStorage.setOrCreate('clawbenchForeground', true);
  }

  onBackground(): void {
    AppStorage.setOrCreate('clawbenchForeground', false);
  }
}
