import { bootstrapApplication } from '@angular/platform-browser';
import { appConfig } from './app/app.config';
import { Sesion } from './app/sesion';

bootstrapApplication(Sesion, appConfig)
  .catch((err) => console.error(err));
