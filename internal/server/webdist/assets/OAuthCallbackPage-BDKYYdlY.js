import{A}from"./Alert-CU0W8A3y.js";import{z as x,A as p,D as E,E as T,d as B,G as O,H as P,o as r,c as v,I as h,J as u,a as b,K as g,l as k,L as I,e as _,T as L,M as H,N as W,y as S,O as D,q as $,P as j,Q as G,S as K,f as M,g as q,v as F,w as y,u as f,C as J,i as Q,m as C,t as z,k as w,B as U}from"./index-CZzX4yFP.js";import{u as X,S as Y,t as Z}from"./text-C4N2r0y-.js";import{_ as ee}from"./_plugin-vue_export-helper-DlAUqK2U.js";var te=x([x("@keyframes spin-rotate",`
 from {
 transform: rotate(0);
 }
 to {
 transform: rotate(360deg);
 }
 `),p("spin-container",`
 position: relative;
 `,[p("spin-body",`
 position: absolute;
 top: 50%;
 left: 50%;
 transform: translateX(-50%) translateY(-50%);
 `,[E()])]),p("spin-body",`
 display: inline-flex;
 align-items: center;
 justify-content: center;
 flex-direction: column;
 `),p("spin",`
 display: inline-flex;
 height: var(--n-size);
 width: var(--n-size);
 font-size: var(--n-size);
 color: var(--n-color);
 `,[T("rotate",`
 animation: spin-rotate 2s linear infinite;
 `)]),p("spin-description",`
 display: inline-block;
 font-size: var(--n-font-size);
 color: var(--n-text-color);
 transition: color .3s var(--n-bezier);
 margin-top: 8px;
 `),p("spin-content",`
 opacity: 1;
 transition: opacity .3s var(--n-bezier);
 pointer-events: all;
 `,[T("spinning",`
 user-select: none;
 -webkit-user-select: none;
 pointer-events: none;
 opacity: var(--n-opacity-spinning);
 `)])]);const se={small:20,medium:18,large:16},ne={...P.props,contentClass:String,contentStyle:[Object,String],description:String,size:{type:[String,Number],default:"medium"},show:{type:Boolean,default:!0},rotate:{type:Boolean,default:!0},spinning:{type:Boolean,validator:()=>!0,default:void 0},delay:Number,...O,strokeWidth:Number};var ae=B({name:"Spin",props:ne,slots:Object,setup(e){const{mergedClsPrefixRef:t,inlineThemeDisabled:l}=H(e),n=P("Spin","-spin",te,j,e,t),d=S(()=>{const{size:s}=e,{common:{cubicBezierEaseInOut:i},self:m}=n.value,{opacitySpinning:N,color:R,textColor:V}=m;return{"--n-bezier":i,"--n-opacity-spinning":N,"--n-size":typeof s=="number"?G(s):m[K("size",s)],"--n-color":R,"--n-text-color":V}}),c=l?W("spin",S(()=>{const{size:s}=e;return typeof s=="number"?String(s):s[0]}),d,e):void 0,a=X(e,["spinning","show"]),o=$(!1);return D(s=>{let i;if(a.value){const{delay:m}=e;if(m){i=window.setTimeout(()=>{o.value=!0},m),s(()=>{clearTimeout(i)});return}}o.value=a.value}),{mergedClsPrefix:t,active:o,mergedStrokeWidth:S(()=>{const{strokeWidth:s}=e;if(s!==void 0)return s;const{size:i}=e;return se[typeof i=="number"?"medium":i]}),cssVars:l?void 0:d,themeClass:c?.themeClass,onRender:c?.onRender}},render(){const{$slots:e,mergedClsPrefix:t,description:l}=this,n=e.icon&&this.rotate,d=(l||e.description)&&(r(),v("div",{class:u(`${t}-spin-description`)},[h(()=>l||e.description?.())],2)),c=e.icon?(r(),v("div",{key:1,class:u([`${t}-spin-body`,this.themeClass])},[b("div",{class:u([`${t}-spin`,n&&`${t}-spin--rotate`]),style:g(e.default?"":this.cssVars)},[h(()=>e.icon())],6),h(()=>d)],2)):(r(),v("div",{key:2,class:u([`${t}-spin-body`,this.themeClass])},[(r(),k(I,{clsPrefix:t,style:g(e.default?"":this.cssVars),stroke:this.stroke,"stroke-width":this.mergedStrokeWidth,radius:this.radius,scale:this.scale,class:u(`${t}-spin`)},null,8,["clsPrefix","style","stroke","stroke-width","radius","scale","class"])),h(()=>d)],2));return this.onRender?.(),e.default?(r(),v("div",{key:3,class:u([`${t}-spin-container`,this.themeClass]),style:g(this.cssVars)},[b("div",{class:u([`${t}-spin-content`,this.active&&`${t}-spin-content--spinning`,this.contentClass]),style:g(this.contentStyle)},[h(()=>e.default?.())],6),_(L,{name:"fade-in-transition"},{default:()=>this.active?c:null},1024)],6)):c}});const ie={class:"auth-page"},oe={class:"callback-copy"},re={class:"callback-title"},le=B({__name:"OAuthCallbackPage",setup(e){const t=M(),l=Q(),n=$("");function d(){const a=window.location.hash.replace(/^#/,"");if(!a)return null;const o=new URLSearchParams(a),s=o.get("access_token"),i=o.get("refresh_token");return!s||!i?null:{access_token:s,refresh_token:i}}async function c(){await l.replace({path:"/login",query:{error:"oauth_failed"}})}return q(async()=>{try{const a=d();if(!a){await c();return}t.setSession(a),window.history.replaceState(null,"","/oauth/callback"),await l.replace({path:"/dashboard"})}catch(a){n.value=F(a)}}),(a,o)=>(r(),v("div",ie,[_(f(J),{class:"auth-card"},{default:y(()=>[_(f(Y),{vertical:"",align:"center",size:16,class:"callback-body"},{default:y(()=>[n.value?C("",!0):(r(),k(f(ae),{key:0,size:"large"})),b("div",oe,[b("h2",re,z(n.value?"Sign-in failed":"Signing you in"),1),_(f(Z),{depth:"3"},{default:y(()=>[w(z(n.value?"GitHub did not return a usable session.":"Completing GitHub sign-in…"),1)]),_:1})]),n.value?(r(),k(f(A),{key:1,type:"error","show-icon":!0},{default:y(()=>[w(z(n.value),1)]),_:1})):C("",!0),n.value?(r(),k(f(U),{key:2,onClick:c},{default:y(()=>[...o[0]||(o[0]=[w(" Back to sign in ",-1)])]),_:1})):C("",!0)]),_:1})]),_:1})]))}}),fe=ee(le,[["__scopeId","data-v-954d908c"]]);export{fe as default};
