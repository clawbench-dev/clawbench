import { UIAbility, ConfigurationConstant } from '@kit.AbilityKit';
import { window } from '@kit.ArkUI';

export default class EntryAbility extends UIAbility {
  onCreate(): void {
    this.context.getApplicationContext().setColorMode(ConfigurationConstant.ColorMode.COLOR_MODE_DARK);
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
