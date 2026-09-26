import{u as R,S as B,t as N}from"./text-ChbxifB-.js";import{v as P,F as S,z as u,ax as V,D as b,d as _,ay as L,G as C,g as p,c as m,O as h,Q as d,Y as k,af as g,h as O,a8 as j,a as y,at as I,I as W,K as D,L as v,aj as E,r as H,az as M,N as K,u as A,o as F,w as x,b as z,f as Y,j as q}from"./index-CoaJi1Aw.js";function G(e){const{opacityDisabled:t,heightTiny:a,heightSmall:r,heightMedium:s,heightLarge:i,heightHuge:l,primaryColor:o,fontSize:n}=e;return{fontSize:n,textColor:o,sizeTiny:a,sizeSmall:r,sizeMedium:s,sizeLarge:i,sizeHuge:l,color:o,opacitySpinning:t}}const Q={common:P,self:G};var U=S([S("@keyframes spin-rotate",`
 from {
 transform: rotate(0);
 }
 to {
 transform: rotate(360deg);
 }
 `),u("spin-container",`
 position: relative;
 `,[u("spin-body",`
 position: absolute;
 top: 50%;
 left: 50%;
 transform: translateX(-50%) translateY(-50%);
 `,[V()])]),u("spin-body",`
 display: inline-flex;
 align-items: center;
 justify-content: center;
 flex-direction: column;
 `),u("spin",`
 display: inline-flex;
 height: var(--n-size);
 width: var(--n-size);
 font-size: var(--n-size);
 color: var(--n-color);
 `,[b("rotate",`
 animation: spin-rotate 2s linear infinite;
 `)]),u("spin-description",`
 display: inline-block;
 font-size: var(--n-font-size);
 color: var(--n-text-color);
 transition: color .3s var(--n-bezier);
 margin-top: 8px;
 `),u("spin-content",`
 opacity: 1;
 transition: opacity .3s var(--n-bezier);
 pointer-events: all;
 `,[b("spinning",`
 user-select: none;
 -webkit-user-select: none;
 pointer-events: none;
 opacity: var(--n-opacity-spinning);
 `)])]);const X={small:20,medium:18,large:16},J={...C.props,contentClass:String,contentStyle:[Object,String],description:String,size:{type:[String,Number],default:"medium"},show:{type:Boolean,default:!0},rotate:{type:Boolean,default:!0},spinning:{type:Boolean,validator:()=>!0,default:void 0},delay:Number,...L,strokeWidth:Number};var Z=_({name:"Spin",props:J,slots:Object,setup(e){const{mergedClsPrefixRef:t,inlineThemeDisabled:a}=W(e),r=C("Spin","-spin",U,Q,e,t),s=v(()=>{const{size:n}=e,{common:{cubicBezierEaseInOut:c},self:f}=r.value,{opacitySpinning:w,color:T,textColor:$}=f;return{"--n-bezier":c,"--n-opacity-spinning":w,"--n-size":typeof n=="number"?M(n):f[K("size",n)],"--n-color":T,"--n-text-color":$}}),i=a?D("spin",v(()=>{const{size:n}=e;return typeof n=="number"?String(n):n[0]}),s,e):void 0,l=R(e,["spinning","show"]),o=H(!1);return E(n=>{let c;if(l.value){const{delay:f}=e;if(f){c=window.setTimeout(()=>{o.value=!0},f),n(()=>{clearTimeout(c)});return}}o.value=l.value}),{mergedClsPrefix:t,active:o,mergedStrokeWidth:v(()=>{const{strokeWidth:n}=e;if(n!==void 0)return n;const{size:c}=e;return X[typeof c=="number"?"medium":c]}),cssVars:a?void 0:s,themeClass:i?.themeClass,onRender:i?.onRender}},render(){const{$slots:e,mergedClsPrefix:t,description:a}=this,r=e.icon&&this.rotate,s=(a||e.description)&&(p(),m("div",{class:d(`${t}-spin-description`)},[h(()=>a||e.description?.())],2)),i=e.icon?(p(),m("div",{key:1,class:d([`${t}-spin-body`,this.themeClass])},[k("div",{class:d([`${t}-spin`,r&&`${t}-spin--rotate`]),style:g(e.default?"":this.cssVars)},[h(()=>e.icon())],6),h(()=>s)],2)):(p(),m("div",{key:2,class:d([`${t}-spin-body`,this.themeClass])},[(p(),O(j,{clsPrefix:t,style:g(e.default?"":this.cssVars),stroke:this.stroke,"stroke-width":this.mergedStrokeWidth,radius:this.radius,scale:this.scale,class:d(`${t}-spin`)},null,8,["clsPrefix","style","stroke","stroke-width","radius","scale","class"])),h(()=>s)],2));return this.onRender?.(),e.default?(p(),m("div",{key:3,class:d([`${t}-spin-container`,this.themeClass]),style:g(this.cssVars)},[k("div",{class:d([`${t}-spin-content`,this.active&&`${t}-spin-content--spinning`,this.contentClass]),style:g(this.contentStyle)},[h(()=>e.default?.())],6),y(I,{name:"fade-in-transition"},{default:()=>this.active?i:null},1024)],6)):i}});const ee={class:"auth-page"},ne=_({__name:"OAuthCallbackPage",setup(e){const t=A(),a=Y();function r(){const s=window.location.hash.replace(/^#/,"");if(!s)return null;const i=new URLSearchParams(s),l=i.get("access_token"),o=i.get("refresh_token");return!l||!o?null:{access_token:l,refresh_token:o}}return F(async()=>{const s=r();if(!s){await a.replace({path:"/login",query:{error:"oauth_failed"}});return}t.setSession(s),window.history.replaceState(null,"","/oauth/callback"),await a.replace({path:"/dashboard"})}),(s,i)=>(p(),m("div",ee,[y(z(B),{vertical:"",align:"center",size:16},{default:x(()=>[y(z(Z),{size:"large"}),y(z(N),{depth:"3"},{default:x(()=>[...i[0]||(i[0]=[q("Signing you in…",-1)])]),_:1})]),_:1})]))}});export{ne as default};
