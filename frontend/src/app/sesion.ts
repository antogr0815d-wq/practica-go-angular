import { HttpClient, HttpErrorResponse } from '@angular/common/http';
import { ChangeDetectionStrategy, Component, OnInit, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { firstValueFrom } from 'rxjs';
import { App } from './app';

interface Identidad {
  usuario: string;
}

@Component({
  selector: 'app-root',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [App, FormsModule],
  templateUrl: './sesion.html',
  styleUrl: './sesion.css',
})
export class Sesion implements OnInit {
  private readonly http = inject(HttpClient);

  protected readonly cuenta = signal<Identidad | null>(null);
  protected readonly usuario = signal('');
  protected readonly password = signal('');
  protected readonly verificando = signal(true);
  protected readonly entrando = signal(false);
  protected readonly cerrando = signal(false);
  protected readonly aviso = signal('');

  async ngOnInit() {
    try {
      this.cuenta.set(await firstValueFrom(this.http.get<Identidad>('/sesion')));
    } catch (error) {
      if (!(error instanceof HttpErrorResponse) || error.status !== 401) {
        this.aviso.set('No se pudo comprobar la sesión. Intenta iniciar sesión.');
      }
    } finally {
      this.verificando.set(false);
    }
  }

  protected async entrar() {
    const usuario = this.usuario().trim();
    const password = this.password();
    if (!usuario || !password || this.entrando()) return;

    this.aviso.set('');
    this.entrando.set(true);
    try {
      this.cuenta.set(await firstValueFrom(this.http.post<Identidad>('/login', { usuario, password })));
    } catch (error) {
      const incorrectos = error instanceof HttpErrorResponse && error.status === 401;
      this.aviso.set(incorrectos ? 'Usuario o contraseña incorrectos.' : 'No se pudo iniciar sesión. Inténtalo de nuevo.');
    } finally {
      this.password.set('');
      this.entrando.set(false);
    }
  }

  protected async salir() {
    if (this.cerrando()) return;

    this.aviso.set('');
    this.cerrando.set(true);
    try {
      await firstValueFrom(this.http.post<void>('/logout', null));
      this.cuenta.set(null);
    } catch {
      this.aviso.set('No se pudo cerrar la sesión. Inténtalo de nuevo.');
    } finally {
      this.cerrando.set(false);
    }
  }

  protected expirar() {
    if (this.cerrando()) return;

    this.cuenta.set(null);
    this.aviso.set('Tu sesión terminó. Vuelve a iniciar sesión.');
  }
}
